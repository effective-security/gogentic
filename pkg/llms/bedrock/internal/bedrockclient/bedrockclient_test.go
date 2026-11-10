package bedrockclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"math"
	"net/http"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testRegion           = "us-east-1"
	testAccessKey        = "AKIDFAKE"
	testSecretKey        = "fake-secret"
	contentTypeJSON      = "application/json"
	contentTypeEvents    = "application/vnd.amazon.eventstream"
	headerContentType    = "Content-Type"
	headerErrorType      = "X-Amzn-Errortype"
	headerAccept         = "Accept"
	headerStreamAccept   = "X-Amzn-Bedrock-Accept"
	acceptAny            = "*/*"
	invokePathFmt        = "/model/%s/invoke"
	invokeStreamPathFmt  = "/model/%s/invoke-with-response-stream"
	validationErrorType  = "ValidationException"
	validationErrMessage = "malformed input"
	validationErrBody    = `{"message":"` + validationErrMessage + `"}`
	invalidJSONBody      = `{not json`

	// AWS event stream wire constants.
	eventHeaderString  = 7
	eventPreludeLength = 12
	eventCRCLength     = 4
	eventMessageType   = ":message-type"
	eventEventType     = ":event-type"
	eventContentType   = ":content-type"
	eventExceptionType = ":exception-type"
	eventTypeEvent     = "event"
	eventTypeException = "exception"
	eventTypeChunk     = "chunk"
)

// doFunc adapts a function to the aws.HTTPClient interface.
type doFunc func(*http.Request) (*http.Response, error)

func (f doFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

// invocation is one HTTP request captured by stubRuntime.
type invocation struct {
	method       string
	path         string
	accept       string
	streamAccept string
	contentType  string
	body         string
}

// stubRuntime fakes the Bedrock runtime HTTP API. It records every request
// and replies with the scripted responses in order (the last one repeats).
type stubRuntime struct {
	t           *testing.T
	mu          sync.Mutex
	status      int
	contentType string
	errorType   string
	responses   [][]byte
	transport   error
	calls       []invocation
	// bodyClosed is closed once the client closes a response body.
	bodyClosed chan struct{}
	closeOnce  sync.Once
}

// notifyBody signals stubRuntime.bodyClosed when the client closes the body.
type notifyBody struct {
	io.Reader
	stub *stubRuntime
}

func (b notifyBody) Close() error {
	b.stub.closeOnce.Do(func() {
		close(b.stub.bodyClosed)
	})
	return nil
}

func newStub(t *testing.T, responses ...string) *stubRuntime {
	t.Helper()
	s := &stubRuntime{
		t:           t,
		status:      http.StatusOK,
		contentType: contentTypeJSON,
		bodyClosed:  make(chan struct{}),
	}
	for _, r := range responses {
		s.responses = append(s.responses, []byte(r))
	}
	return s
}

func (s *stubRuntime) do(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(r.Body)
		require.NoError(s.t, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, invocation{
		method:       r.Method,
		path:         r.URL.Path,
		accept:       r.Header.Get(headerAccept),
		streamAccept: r.Header.Get(headerStreamAccept),
		contentType:  r.Header.Get(headerContentType),
		body:         string(body),
	})
	if s.transport != nil {
		return nil, s.transport
	}
	idx := len(s.calls) - 1
	if last := len(s.responses) - 1; idx > last {
		idx = last
	}
	var payload []byte
	if idx >= 0 {
		payload = s.responses[idx]
	}
	header := http.Header{}
	header.Set(headerContentType, s.contentType)
	if s.errorType != "" {
		header.Set(headerErrorType, s.errorType)
	}
	return &http.Response{
		StatusCode: s.status,
		Header:     header,
		Body: notifyBody{
			Reader: bytes.NewReader(payload),
			stub:   s,
		},
		ContentLength: int64(len(payload)),
		Request:       r,
	}, nil
}

func (s *stubRuntime) runtime() *bedrockruntime.Client {
	return bedrockruntime.NewFromConfig(aws.Config{
		Region:           testRegion,
		Credentials:      credentials.NewStaticCredentialsProvider(testAccessKey, testSecretKey, ""),
		HTTPClient:       doFunc(s.do),
		RetryMaxAttempts: 1,
	})
}

func (s *stubRuntime) client() *Client {
	return NewClient(s.runtime())
}

// requests returns a snapshot of the captured requests.
func (s *stubRuntime) requests() []invocation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]invocation(nil), s.calls...)
}

// fail configures the stub to answer with a modeled Bedrock validation error.
func (s *stubRuntime) fail() *stubRuntime {
	s.status = http.StatusBadRequest
	s.errorType = validationErrorType
	s.responses = [][]byte{[]byte(validationErrBody)}
	return s
}

// requireValidationError asserts err is the modeled error produced by stubRuntime.fail.
func requireValidationError(t *testing.T, err error) {
	t.Helper()
	var ve *types.ValidationException
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, validationErrMessage, ve.ErrorMessage())
}

// eventHeader is one string header of an AWS event stream message.
type eventHeader struct {
	name  string
	value string
}

// encodeEvent encodes one AWS event stream message (prelude, string headers,
// payload and CRCs) as Bedrock streams it.
func encodeEvent(headers []eventHeader, payload []byte) []byte {
	var hb []byte
	for _, h := range headers {
		hb = append(hb, byte(len(h.name)))
		hb = append(hb, h.name...)
		hb = append(hb, eventHeaderString)
		hb = binary.BigEndian.AppendUint16(hb, uint16(len(h.value)))
		hb = append(hb, h.value...)
	}
	total := eventPreludeLength + len(hb) + len(payload) + eventCRCLength
	msg := binary.BigEndian.AppendUint32(nil, uint32(total))
	msg = binary.BigEndian.AppendUint32(msg, uint32(len(hb)))
	msg = binary.BigEndian.AppendUint32(msg, crc32.ChecksumIEEE(msg))
	msg = append(msg, hb...)
	msg = append(msg, payload...)
	return binary.BigEndian.AppendUint32(msg, crc32.ChecksumIEEE(msg))
}

// chunkEvent encodes a ResponseStream "chunk" event carrying raw model output bytes.
func chunkEvent(t *testing.T, raw string) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"bytes": base64.StdEncoding.EncodeToString([]byte(raw)),
	})
	require.NoError(t, err)
	return encodeEvent([]eventHeader{
		{
			name:  eventMessageType,
			value: eventTypeEvent,
		},
		{
			name:  eventEventType,
			value: eventTypeChunk,
		},
		{
			name:  eventContentType,
			value: contentTypeJSON,
		},
	}, payload)
}

// typedEvent encodes an event of the given type that the client does not model.
func typedEvent(eventType, payload string) []byte {
	return encodeEvent([]eventHeader{
		{
			name:  eventMessageType,
			value: eventTypeEvent,
		},
		{
			name:  eventEventType,
			value: eventType,
		},
		{
			name:  eventContentType,
			value: contentTypeJSON,
		},
	}, []byte(payload))
}

// exceptionEvent encodes a modeled ResponseStream exception event.
func exceptionEvent(exceptionType, message string) []byte {
	return encodeEvent([]eventHeader{
		{
			name:  eventMessageType,
			value: eventTypeException,
		},
		{
			name:  eventExceptionType,
			value: exceptionType,
		},
		{
			name:  eventContentType,
			value: contentTypeJSON,
		},
	}, []byte(`{"message":"`+message+`"}`))
}

// eventStream concatenates encoded events into one response body.
func eventStream(events ...[]byte) string {
	return string(bytes.Join(events, nil))
}

func textMessage(role llms.Role, text string) Message {
	return Message{
		Role:    role,
		Content: text,
		Type:    AnthropicMessageTypeText,
	}
}

func TestCreateCompletionUnsupportedProvider(t *testing.T) {
	s := newStub(t)
	res, err := s.client().CreateCompletion(context.Background(), "mistral.mistral-7b-instruct-v0:2", nil, llms.CallOptions{})
	require.EqualError(t, err, "bedrock: unsupported provider")
	assert.Nil(t, res)
	assert.Empty(t, s.requests(), "no request is sent for unsupported providers")
}

func TestCreateEmbeddingUnsupportedProvider(t *testing.T) {
	s := newStub(t)
	res, err := s.client().CreateEmbedding(context.Background(), "meta.llama3-8b-instruct-v1:0", []string{"hi"})
	require.EqualError(t, err, "bedrock: unsupported provider for embeddings")
	assert.Nil(t, res)
	assert.Empty(t, s.requests(), "no request is sent for unsupported providers")
}

func TestProcessInputMessagesGeneric(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		expected string
	}{
		{
			name:     "empty",
			messages: nil,
			expected: "",
		},
		{
			name: "no roles concatenates text only",
			messages: []Message{
				{
					Content: "hello ",
					Type:    AnthropicMessageTypeText,
				},
				{
					Content: "ignored",
					Type:    AnthropicMessageTypeImage,
				},
				{
					Content: "world",
					Type:    AnthropicMessageTypeText,
				},
			},
			expected: "hello world",
		},
		{
			name: "roles add speaker prefixes and the AI prompt",
			messages: []Message{
				textMessage(llms.RoleSystem, "be brief"),
				textMessage(llms.RoleHuman, "hi"),
				{
					Role:    llms.RoleHuman,
					Content: "binary",
					Type:    AnthropicMessageTypeImage,
				},
			},
			expected: "\nsystem: be brief\nhuman: hi\nhuman: \nAI: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, processInputMessagesGeneric(tt.messages))
		})
	}
}

func TestGetMaxTokens(t *testing.T) {
	assert.Equal(t, 42, getMaxTokens(0, 42))
	assert.Equal(t, 42, getMaxTokens(-1, 42))
	assert.Equal(t, 7, getMaxTokens(7, 42))
}

func TestCompletionRequestEncodingError(t *testing.T) {
	msgs := []Message{
		textMessage(llms.RoleHuman, "hi"),
	}
	opts := llms.CallOptions{
		Temperature: math.NaN(),
	}
	for _, modelID := range []string{
		"ai21.j2-mid-v1",
		"amazon.titan-text-lite-v1",
		"anthropic.claude-3-haiku-20240307-v1:0",
		"cohere.command-text-v14",
		"meta.llama3-8b-instruct-v1:0",
	} {
		t.Run(modelID, func(t *testing.T) {
			s := newStub(t)
			res, err := s.client().CreateCompletion(context.Background(), modelID, msgs, opts)
			require.EqualError(t, err, "json: unsupported value: NaN")
			assert.Nil(t, res)
			assert.Empty(t, s.requests(), "nothing is sent when the body cannot be encoded")
		})
	}
}

func TestTransportErrorIsReturned(t *testing.T) {
	transportErr := errors.New("connection refused")
	s := newStub(t)
	s.transport = transportErr
	_, err := s.client().CreateCompletion(context.Background(), "meta.llama3-8b-instruct-v1:0", []Message{
		textMessage(llms.RoleHuman, "hi"),
	}, llms.CallOptions{})
	require.ErrorIs(t, err, transportErr)
	assert.Len(t, s.requests(), 1, "retries are disabled")
}
