package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/mcp/internal/protocol"
	"github.com/effective-security/gogentic/mcp/transport"
	"github.com/effective-security/x/maps"
	"github.com/effective-security/xlog"
	"github.com/invopop/jsonschema"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "mcp")

// Here we define the actual MCP server that users will create and run
// A server can be passed a number of handlers to handle requests from clients
// Additionally it can be parametrized by a transport. This transport will be used to actually send and receive messages.
// So for example if the stdio transport is used, the server will read from stdin and write to stdout
// If the SSE transport is used, the server will send messages over an SSE connection and receive messages from HTTP POST requests.

// The interface that we're looking to support is something like [gin](https://github.com/gin-gonic/gin)s interface

type toolResponseSent struct {
	Response *ToolResponse
	Error    error
}

type CallToolResult struct {
	// This result property is reserved by the protocol to allow clients and servers
	// to attach additional metadata to their responses.
	Meta map[string]any `json:"_meta,omitempty" yaml:"_meta,omitempty" mapstructure:"_meta,omitempty"`

	// Content corresponds to the JSON schema field "content".
	Content []*Content `json:"content" yaml:"content" mapstructure:"content"`

	// Whether the tool call ended in an error.
	//
	// If not set, this is assumed to be false (the call was successful).
	IsError *bool `json:"isError,omitempty" yaml:"isError,omitempty" mapstructure:"isError,omitempty"`

	// NEW in MCP v2025-06-18
	StructuredContent any `json:"structuredContent,omitempty" yaml:"structuredContent,omitempty" mapstructure:"structuredContent,omitempty"`
}

// MarshalJSON encodes the tools/call result. A handler error is sent as text
// content with isError set; a response the handler flagged with IsError is
// sent with isError set as well.
func (c toolResponseSent) MarshalJSON() ([]byte, error) {
	if c.Error != nil {
		c.Response = NewToolResponse(NewTextContent(c.Error.Error()))
		c.Response.IsError = true
	}
	r := CallToolResult{
		Content:           c.Response.Content,
		StructuredContent: c.Response.StructuredContent,
		Meta:              c.Response.Meta,
	}
	if c.Response.IsError {
		r.IsError = &c.Response.IsError
	}
	return json.Marshal(r)
}

// Custom JSON marshaling for ToolResponse
func (c resourceResponseSent) MarshalJSON() ([]byte, error) {
	if c.Error != nil {
		errorText := c.Error.Error()
		c.Response = NewResourceResponse(NewTextEmbeddedResource(c.Uri, errorText, "text/plain"))
	}
	return json.Marshal(c.Response)
}

type resourceResponseSent struct {
	Response *ResourceResponse
	Uri      string
	Error    error
}

// newResourceResponseSentError creates the response to a failed read of the
// resource at uri; it is sent as a text resource at that uri.
func newResourceResponseSentError(uri string, err error) *resourceResponseSent {
	return &resourceResponseSent{
		Uri:   uri,
		Error: err,
	}
}

// newToolResponseSent creates a new toolResponseSent
func newResourceResponseSent(response *ResourceResponse) *resourceResponseSent {
	return &resourceResponseSent{
		Response: response,
	}
}

type promptResponseSent struct {
	Response *PromptResponse
	Error    error
}

func newPromptResponseSentError(err error) *promptResponseSent {
	return &promptResponseSent{
		Error: err,
	}
}

// newToolResponseSent creates a new toolResponseSent
func newPromptResponseSent(response *PromptResponse) *promptResponseSent {
	return &promptResponseSent{
		Response: response,
	}
}

// Custom JSON marshaling for PromptResponse
func (c promptResponseSent) MarshalJSON() ([]byte, error) {
	if c.Error != nil {
		errorText := c.Error.Error()
		c.Response = NewPromptResponse("error", NewPromptMessage(NewTextContent(errorText), RoleUser))
	}
	return json.Marshal(c.Response)
}

type Server struct {
	isRunning          bool
	transport          transport.Transport
	protocol           *protocol.Protocol
	paginationLimit    *int
	tools              *maps.SyncMap[string, *tool]
	prompts            *maps.SyncMap[string, *prompt]
	resources          *maps.SyncMap[string, *resource]
	resourceTemplates  *maps.SyncMap[string, *resourceTemplate]
	serverInstructions *string
	serverName         string
	serverVersion      string
}

type prompt struct {
	Name              string
	Description       string
	Handler           func(context.Context, baseGetPromptRequestParamsArguments) *promptResponseSent
	PromptInputSchema *PromptSchema
}

type tool struct {
	Name            string
	Description     string
	Handler         func(context.Context, baseCallToolRequestParams) *toolResponseSent
	ToolInputSchema *jsonschema.Schema
}

type resource struct {
	Name        string
	Description string
	Uri         string
	mimeType    string
	Handler     func(context.Context) *resourceResponseSent
}

type resourceTemplate struct {
	Name        string
	Description string
	UriTemplate string
	MimeType    string
}

type ServerOptions func(*Server)

func WithProtocol(protocol *protocol.Protocol) ServerOptions {
	return func(s *Server) {
		s.protocol = protocol
	}
}

// WithPaginationLimit caps the number of items returned per page by tools/list,
// prompts/list, resources/list and resources/templates/list. A full page
// carries a nextCursor; a non-positive limit disables pagination.
//
// Beware: As of 2024-12-13, it looks like Claude does not support pagination yet
func WithPaginationLimit(limit int) ServerOptions {
	return func(s *Server) {
		s.paginationLimit = &limit
	}
}

func WithName(name string) ServerOptions {
	return func(s *Server) {
		s.serverName = name
	}
}

func WithInstructions(instructions string) ServerOptions {
	return func(s *Server) {
		instructionsCopy := instructions
		s.serverInstructions = &instructionsCopy
	}
}

func WithVersion(version string) ServerOptions {
	return func(s *Server) {
		s.serverVersion = version
	}
}

func NewServer(transport transport.Transport, options ...ServerOptions) *Server {
	server := &Server{
		protocol:          protocol.NewProtocol(nil),
		transport:         transport,
		tools:             new(maps.SyncMap[string, *tool]),
		prompts:           new(maps.SyncMap[string, *prompt]),
		resources:         new(maps.SyncMap[string, *resource]),
		resourceTemplates: new(maps.SyncMap[string, *resourceTemplate]),
	}
	for _, option := range options {
		option(server)
	}
	return server
}

// RegisterTool registers a tool whose handler is
// func([context.Context,] [T]) (*ToolResponse, error). The input schema of
// the tool is reflected from T; a handler without T takes an empty object. A
// handler of another signature, or a nil one, is rejected, and so is a
// recursive T, with an error wrapping schema.ErrRecursiveType.
func (s *Server) RegisterTool(name string, description string, handler any) error {
	h, err := parseHandler(handlerKindTool, name, handler, toolResponseType)
	if err != nil {
		return err
	}
	inputSchema, err := argumentsSchema(h.argType)
	if err != nil {
		return errors.Wrapf(err, "failed to register tool %q", name)
	}

	s.tools.Store(name, &tool{
		Name:            name,
		Description:     description,
		Handler:         newToolCall(h),
		ToolInputSchema: inputSchema,
	})

	return s.sendToolListChangedNotification()
}

func (s *Server) sendToolListChangedNotification() error {
	if !s.isRunning {
		return nil
	}
	return s.protocol.Notification("notifications/tools/list_changed", nil)
}

func (s *Server) CheckToolRegistered(name string) bool {
	_, ok := s.tools.Load(name)
	return ok
}

func (s *Server) DeregisterTool(name string) error {
	s.tools.Delete(name)
	return s.sendToolListChangedNotification()
}

// RegisterResource registers the resource at uri, whose handler is
// func([context.Context]) (*ResourceResponse, error). A handler of another
// signature, or a nil one, is rejected.
func (s *Server) RegisterResource(uri string, name string, description string, mimeType string, handler any) error {
	h, err := parseResourceHandler(uri, handler)
	if err != nil {
		return err
	}
	s.resources.Store(uri, &resource{
		Name:        name,
		Description: description,
		Uri:         uri,
		mimeType:    mimeType,
		Handler:     newResourceRead(h),
	})
	return s.sendResourceListChangedNotification()
}

func (s *Server) sendResourceListChangedNotification() error {
	if !s.isRunning {
		return nil
	}
	return s.protocol.Notification("notifications/resources/list_changed", nil)
}

func (s *Server) CheckResourceRegistered(uri string) bool {
	_, ok := s.resources.Load(uri)
	return ok
}

func (s *Server) DeregisterResource(uri string) error {
	s.resources.Delete(uri)
	return s.sendResourceListChangedNotification()
}

func (s *Server) RegisterResourceTemplate(uriTemplate string, name string, description string, mimeType string) error {
	s.resourceTemplates.Store(uriTemplate, &resourceTemplate{
		Name:        name,
		Description: description,
		UriTemplate: uriTemplate,
		MimeType:    mimeType,
	})
	return s.sendResourceListChangedNotification()
}

func (s *Server) CheckResourceTemplateRegistered(uriTemplate string) bool {
	_, ok := s.resourceTemplates.Load(uriTemplate)
	return ok
}

func (s *Server) DeregisterResourceTemplate(uriTemplate string) error {
	s.resourceTemplates.Delete(uriTemplate)
	return s.sendResourceListChangedNotification()
}

// RegisterPrompt registers a prompt whose handler is
// func([context.Context,] [T]) (*PromptResponse, error), where T is a struct
// of string or *string fields, one per prompt argument; a handler without T
// takes no arguments. A handler of another signature, or a nil one, is
// rejected, and so is a T reflected as a recursive type (through a
// JSONSchemaAlias method), with an error wrapping schema.ErrRecursiveType.
func (s *Server) RegisterPrompt(name string, description string, handler any) error {
	h, err := parsePromptHandler(name, handler)
	if err != nil {
		return err
	}
	promptSchema, err := newPromptSchema(name, description, h.argType)
	if err != nil {
		return errors.Wrapf(err, "failed to register prompt %q", name)
	}
	s.prompts.Store(name, &prompt{
		Name:              name,
		Description:       description,
		Handler:           newPromptCall(h),
		PromptInputSchema: promptSchema,
	})

	return s.sendPromptListChangedNotification()
}

func (s *Server) sendPromptListChangedNotification() error {
	if !s.isRunning {
		return nil
	}
	return s.protocol.Notification("notifications/prompts/list_changed", nil)
}

func (s *Server) CheckPromptRegistered(name string) bool {
	_, ok := s.prompts.Load(name)
	return ok
}

func (s *Server) DeregisterPrompt(name string) error {
	s.prompts.Delete(name)
	return s.sendPromptListChangedNotification()
}

func (s *Server) Serve() error {
	if s.isRunning {
		return errors.Errorf("server is already running")
	}
	pr := s.protocol
	pr.SetRequestHandler("ping", s.handlePing)
	pr.SetRequestHandler("initialize", s.handleInitialize)
	pr.SetRequestHandler("tools/list", s.handleListTools)
	pr.SetRequestHandler("tools/call", s.handleToolCalls)
	pr.SetRequestHandler("prompts/list", s.handleListPrompts)
	pr.SetRequestHandler("prompts/get", s.handlePromptCalls)
	pr.SetRequestHandler("resources/list", s.handleListResources)
	pr.SetRequestHandler("resources/templates/list", s.handleListResourceTemplates)
	pr.SetRequestHandler("resources/read", s.handleResourceCalls)
	err := pr.Connect(s.transport)
	if err != nil {
		return err
	}
	s.protocol = pr
	s.isRunning = true
	return nil
}

func (s *Server) handleInitialize(ctx context.Context, request *transport.BaseJSONRPCRequest, _ protocol.RequestHandlerExtra) (transport.JsonRpcBody, error) {
	return InitializeResponse{
		Meta:            nil,
		Capabilities:    s.generateCapabilities(),
		Instructions:    s.serverInstructions,
		ProtocolVersion: "2025-06-18",
		ServerInfo: implementation{
			Name:    s.serverName,
			Version: s.serverVersion,
		},
	}, nil
}

func (s *Server) handleListTools(ctx context.Context, request *transport.BaseJSONRPCRequest, _ protocol.RequestHandlerExtra) (transport.JsonRpcBody, error) {
	page, nextCursor, err := paginate(request, s.tools, s.paginationLimit)
	if err != nil {
		return nil, err
	}

	toolsToReturn := make([]ToolRetType, 0, len(page))
	for _, t := range page {
		toolsToReturn = append(toolsToReturn, ToolRetType{
			Name:        t.Name,
			Description: &t.Description,
			InputSchema: t.ToolInputSchema,
		})
	}

	return ToolsResponse{
		Tools:      toolsToReturn,
		NextCursor: nextCursor,
	}, nil
}

func (s *Server) handleToolCalls(ctx context.Context, req *transport.BaseJSONRPCRequest, _ protocol.RequestHandlerExtra) (transport.JsonRpcBody, error) {
	params := baseCallToolRequestParams{}
	// Instantiate a struct of the type of the arguments
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal arguments")
	}

	toolToUse, ok := s.tools.Load(params.Name)
	if toolToUse == nil || !ok {
		return nil, errors.Errorf("unknown tool: %s", params.Name)
	}
	return toolToUse.Handler(ctx, params), nil
}

func (s *Server) generateCapabilities() ServerCapabilities {
	t := false
	return ServerCapabilities{
		Tools: func() *ServerCapabilitiesTools {
			return &ServerCapabilitiesTools{
				ListChanged: &t,
			}
		}(),
		Prompts: func() *ServerCapabilitiesPrompts {
			return &ServerCapabilitiesPrompts{
				ListChanged: &t,
			}
		}(),
		Resources: func() *ServerCapabilitiesResources {
			return &ServerCapabilitiesResources{
				ListChanged: &t,
			}
		}(),
	}
}
func (s *Server) handleListPrompts(ctx context.Context, request *transport.BaseJSONRPCRequest, extra protocol.RequestHandlerExtra) (transport.JsonRpcBody, error) {
	page, nextCursor, err := paginate(request, s.prompts, s.paginationLimit)
	if err != nil {
		return nil, err
	}

	// The schemas are complete at registration and shared by concurrent
	// listings, so they are never modified here.
	promptsToReturn := make([]*PromptSchema, 0, len(page))
	for _, p := range page {
		promptsToReturn = append(promptsToReturn, p.PromptInputSchema)
	}

	return ListPromptsResponse{
		Prompts:    promptsToReturn,
		NextCursor: nextCursor,
	}, nil
}

func (s *Server) handleListResources(ctx context.Context, request *transport.BaseJSONRPCRequest, extra protocol.RequestHandlerExtra) (transport.JsonRpcBody, error) {
	page, nextCursor, err := paginate(request, s.resources, s.paginationLimit)
	if err != nil {
		return nil, err
	}

	resourcesToReturn := make([]*ResourceSchema, 0, len(page))
	for _, r := range page {
		resourcesToReturn = append(resourcesToReturn, &ResourceSchema{
			Annotations: nil,
			Description: &r.Description,
			MimeType:    &r.mimeType,
			Name:        r.Name,
			Uri:         r.Uri,
		})
	}

	return ListResourcesResponse{
		Resources:  resourcesToReturn,
		NextCursor: nextCursor,
	}, nil
}

func (s *Server) handleListResourceTemplates(ctx context.Context, request *transport.BaseJSONRPCRequest, extra protocol.RequestHandlerExtra) (transport.JsonRpcBody, error) {
	page, nextCursor, err := paginate(request, s.resourceTemplates, s.paginationLimit)
	if err != nil {
		return nil, err
	}

	templatesToReturn := make([]*ResourceTemplateSchema, 0, len(page))
	for _, t := range page {
		templatesToReturn = append(templatesToReturn, &ResourceTemplateSchema{
			Annotations: nil,
			Description: &t.Description,
			MimeType:    &t.MimeType,
			Name:        t.Name,
			UriTemplate: t.UriTemplate,
		})
	}

	return ListResourceTemplatesResponse{
		Templates:  templatesToReturn,
		NextCursor: nextCursor,
	}, nil
}

// paginate returns the page of a */list request: the values of items ordered
// by key that follow the request's cursor, at most limit of them, and the
// cursor of the next page. A nil or non-positive limit returns every remaining
// value and no cursor.
//
// The cursor is the base64-encoded key of the last value of the previous page.
// A full page always carries a cursor, and a cursor at or past the last key
// yields an empty page without one, so following cursors until none is
// returned visits every value exactly once.
func paginate[V any](request *transport.BaseJSONRPCRequest, items *maps.SyncMap[string, V], limit *int) ([]V, *string, error) {
	var params struct {
		Cursor *string `json:"cursor"`
	}
	if len(request.Params) > 0 {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, nil, errors.WithMessage(err, "failed to unmarshal arguments")
		}
	}

	type entry struct {
		key   string
		value V
	}
	var entries []entry
	items.Range(func(key string, value V) bool {
		entries = append(entries, entry{
			key:   key,
			value: value,
		})
		return true
	})
	slices.SortFunc(entries, func(a, b entry) int {
		return strings.Compare(a.key, b.key)
	})

	start := 0
	if params.Cursor != nil {
		c, err := base64.StdEncoding.DecodeString(*params.Cursor)
		if err != nil {
			return nil, nil, errors.WithMessage(err, "failed to decode cursor")
		}
		last := string(c)
		start = sort.Search(len(entries), func(i int) bool {
			return entries[i].key > last
		})
	}

	end := len(entries)
	paged := limit != nil && *limit > 0
	if paged {
		end = min(end, start+*limit)
	}

	page := make([]V, 0, end-start)
	for _, e := range entries[start:end] {
		page = append(page, e.value)
	}

	if !paged || len(page) < *limit {
		return page, nil, nil
	}
	nextCursor := base64.StdEncoding.EncodeToString([]byte(entries[end-1].key))
	return page, &nextCursor, nil
}

func (s *Server) handlePromptCalls(ctx context.Context, req *transport.BaseJSONRPCRequest, extra protocol.RequestHandlerExtra) (transport.JsonRpcBody, error) {
	params := baseGetPromptRequestParamsArguments{}
	// Instantiate a struct of the type of the arguments
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal arguments")
	}

	promptToUse, ok := s.prompts.Load(params.Name)
	if !ok {
		return nil, errors.Errorf("unknown prompt: %s", params.Name)
	}
	return promptToUse.Handler(ctx, params), nil
}

func (s *Server) handleResourceCalls(ctx context.Context, req *transport.BaseJSONRPCRequest, extra protocol.RequestHandlerExtra) (transport.JsonRpcBody, error) {
	params := readResourceRequestParams{}
	// Instantiate a struct of the type of the arguments
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal arguments")
	}

	resourceToUse, ok := s.resources.Load(params.Uri)
	if !ok {
		return nil, errors.Errorf("unknown resource: %s", params.Uri)
	}
	return resourceToUse.Handler(ctx), nil
}

func (s *Server) handlePing(ctx context.Context, request *transport.BaseJSONRPCRequest, extra protocol.RequestHandlerExtra) (transport.JsonRpcBody, error) {
	return map[string]any{}, nil
}

var (
	jsonSchemaReflector = jsonschema.Reflector{
		BaseSchemaID:               "",
		Anonymous:                  true,
		AssignAnchor:               false,
		AllowAdditionalProperties:  true,
		RequiredFromJSONSchemaTags: false, // either use jsonschema required tags or json omitempty tags
		DoNotReference:             true,
		ExpandedStruct:             true,
		FieldNameTag:               "",
		IgnoredTypes:               nil,
		Lookup:                     nil,
		Mapper:                     nil,
		Namer:                      nil,
		KeyNamer:                   nil,
		AdditionalFields:           nil,
		CommentMap:                 nil,
	}
)
