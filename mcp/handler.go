package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"runtime/debug"
	"slices"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	"github.com/invopop/jsonschema"
)

// Kinds of user handlers, named in error messages and logs.
const (
	handlerKindTool     = "tool"
	handlerKindPrompt   = "prompt"
	handlerKindResource = "resource"
)

// jsonTypeObject is the JSON Schema type of handler arguments.
const jsonTypeObject = "object"

var (
	contextType          = reflect.TypeFor[context.Context]()
	errorType            = reflect.TypeFor[error]()
	toolResponseType     = reflect.TypeFor[*ToolResponse]()
	promptResponseType   = reflect.TypeFor[*PromptResponse]()
	resourceResponseType = reflect.TypeFor[*ResourceResponse]()
)

// handler is a user handler of a tool, prompt or resource whose signature
// was accepted by validateToolHandler, validatePromptHandler or
// validateResourceHandler.
type handler struct {
	kind string
	fn   reflect.Value
	// takesContext is set when the first argument is a context.Context.
	takesContext bool
	// argType is the type the request arguments are decoded into; nil when
	// the handler takes no arguments.
	argType reflect.Type
}

// newHandler describes a validated handler of the given kind.
func newHandler(kind string, fn any) *handler {
	v := reflect.ValueOf(fn)
	t := v.Type()
	numIn := t.NumIn()
	h := &handler{
		kind: kind,
		fn:   v,
	}
	next := 0
	if numIn > 0 && t.In(0) == contextType {
		h.takesContext = true
		next = 1
	}
	if next < numIn {
		h.argType = t.In(next)
	}
	return h
}

// callHandler runs h for one request and returns the response it produced.
//
// raw holds the request arguments; when they are absent or JSON null, the
// handler receives the zero value of its arguments type. handlerErr is the
// error the handler returned. err reports that the handler could not
// produce a response: the arguments did not decode, the handler panicked,
// or it returned neither a response nor an error. A panic is recovered so
// that a faulty handler fails its request instead of the server.
func callHandler[R any](ctx context.Context, h *handler, raw json.RawMessage) (resp *R, handlerErr error, err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		cause, ok := r.(error)
		if !ok {
			cause = errors.Errorf("%v", r)
		}
		resp, handlerErr = nil, nil
		err = errors.Wrapf(cause, "internal error: %s panicked", h.kind)
		logger.ContextKV(ctx, xlog.ERROR,
			"reason", "panic",
			"handler", h.kind,
			"err", err.Error(),
			"stack", string(debug.Stack()),
		)
	}()

	args := make([]reflect.Value, 0, h.fn.Type().NumIn())
	if h.takesContext {
		args = append(args, reflect.ValueOf(ctx))
	}
	if h.argType != nil {
		arg := reflect.New(h.argType)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, arg.Interface()); err != nil {
				return nil, nil, errors.WithMessage(err, "failed to unmarshal arguments")
			}
		}
		args = append(args, arg.Elem())
	}

	out := h.fn.Call(args)
	if e, _ := out[1].Interface().(error); e != nil {
		return nil, e, nil
	}
	resp, _ = out[0].Interface().(*R)
	if resp == nil {
		return nil, nil, errors.Errorf("%s handler returned neither a response nor an error", h.kind)
	}
	return resp, nil, nil
}

// newToolCall returns the tools/call function of a tool handler. A handler
// error is returned as a tool error result prefixed with
// "failed to handle tool call".
func newToolCall(h *handler) func(context.Context, baseCallToolRequestParams) *toolResponseSent {
	return func(ctx context.Context, params baseCallToolRequestParams) *toolResponseSent {
		resp, handlerErr, err := callHandler[ToolResponse](ctx, h, params.Arguments)
		if handlerErr != nil {
			err = errors.WithMessage(handlerErr, "failed to handle tool call")
		}
		if err != nil {
			return newToolResponseSentError(err)
		}
		return newToolResponseSent(resp)
	}
}

// newPromptCall returns the prompts/get function of a prompt handler.
func newPromptCall(h *handler) func(context.Context, baseGetPromptRequestParamsArguments) *promptResponseSent {
	return func(ctx context.Context, params baseGetPromptRequestParamsArguments) *promptResponseSent {
		resp, handlerErr, err := callHandler[PromptResponse](ctx, h, params.Arguments)
		if err == nil {
			err = handlerErr
		}
		if err != nil {
			return newPromptResponseSentError(err)
		}
		return newPromptResponseSent(resp)
	}
}

// newResourceRead returns the resources/read function of the handler of the
// resource at uri. An error is sent as a text resource at the same uri.
func newResourceRead(uri string, h *handler) func(context.Context) *resourceResponseSent {
	return func(ctx context.Context) *resourceResponseSent {
		resp, handlerErr, err := callHandler[ResourceResponse](ctx, h, nil)
		if err == nil {
			err = handlerErr
		}
		if err != nil {
			return newResourceResponseSentError(uri, err)
		}
		return newResourceResponseSent(resp)
	}
}

// funcTypeOf returns the type of a handler, or an error when the handler is
// not a function the server can call with a fixed argument list.
func funcTypeOf(handler any) (reflect.Type, error) {
	t := reflect.TypeOf(handler)
	if t == nil || t.Kind() != reflect.Func {
		return nil, errors.Errorf("handler must be a function, got %T", handler)
	}
	if t.IsVariadic() {
		return nil, errors.Errorf("handler must not be variadic, got %s", t)
	}
	return t, nil
}

// validateResults checks that a handler of type t returns (want, error).
func validateResults(t, want reflect.Type) error {
	if numOut := t.NumOut(); numOut != 2 {
		return errors.Errorf("handler must return exactly two values, got %d", numOut)
	}
	if out := t.Out(0); out != want {
		return errors.Errorf("handler must return %s, got %s", want, out)
	}
	if out := t.Out(1); out != errorType {
		return errors.Errorf("handler must return error, got %s", out)
	}
	return nil
}

// validateToolHandler checks that handler is
// func([context.Context,] T) (*ToolResponse, error).
func validateToolHandler(handler any) error {
	t, err := funcTypeOf(handler)
	if err != nil {
		return err
	}
	numIn := t.NumIn()
	if numIn != 1 && numIn != 2 {
		return errors.Errorf("handler must take exactly one or two arguments, got %d", numIn)
	}
	if numIn == 2 && t.In(0) != contextType {
		return errors.Errorf("when a handler has 2 arguments, handler must take context.Context as the first argument, got %s", t.In(0))
	}
	return validateResults(t, toolResponseType)
}

// validatePromptHandler checks that handler is
// func([context.Context,] T) (*PromptResponse, error), where T is a struct
// whose fields are all string or *string, as prompt arguments are strings.
func validatePromptHandler(handler any) error {
	t, err := funcTypeOf(handler)
	if err != nil {
		return err
	}
	var argType reflect.Type
	switch numIn := t.NumIn(); numIn {
	case 2:
		if first := t.In(0); first != contextType {
			return errors.Errorf("when a handler has 2 arguments, the first argument must be context.Context, got %s", first)
		}
		argType = t.In(1)
	case 1:
		argType = t.In(0)
	default:
		return errors.Errorf("handler must take one or two arguments, got %d", numIn)
	}

	if argType.Kind() != reflect.Struct {
		return errors.New("argument must be a struct")
	}
	for field := range argType.Fields() {
		kind := field.Type.Kind()
		if kind != reflect.String &&
			(kind != reflect.Pointer || field.Type.Elem().Kind() != reflect.String) {
			return errors.Errorf("all fields of the struct must be of type string or *string, found %s", kind)
		}
	}
	return validateResults(t, promptResponseType)
}

// validateResourceHandler checks that handler is
// func([context.Context]) (*ResourceResponse, error).
func validateResourceHandler(handler any) error {
	t, err := funcTypeOf(handler)
	if err != nil {
		return err
	}
	numIn := t.NumIn()
	if numIn > 1 {
		return errors.Errorf("handler must take no or one arguments, got %d", numIn)
	}
	if numIn == 1 && t.In(0) != contextType {
		return errors.Errorf("when a handler has 1 argument, it must be context.Context, got %s", t.In(0))
	}
	return validateResults(t, resourceResponseType)
}

// argumentsSchema returns the JSON Schema of a handler's arguments type,
// derived by reflection; jsonschema tags supply descriptions and required
// fields. A handler without arguments takes an empty object.
func argumentsSchema(argType reflect.Type) *jsonschema.Schema {
	if argType == nil {
		return &jsonschema.Schema{
			Type:       jsonTypeObject,
			Properties: jsonschema.NewProperties(),
		}
	}
	return jsonSchemaReflector.ReflectFromType(argType)
}

// newPromptSchema describes a prompt for prompts/list: its name, description
// and one argument per field of the handler's arguments struct.
//
// Example:
//
//	type Content struct {
//		Title       string  `json:"title" jsonschema:"description=The title to submit,required"`
//		Description *string `json:"description" jsonschema:"description=The description to submit"`
//	}
//
// gives a required "title" argument and an optional "description" argument.
func newPromptSchema(name, description string, argType reflect.Type) *PromptSchema {
	inputSchema := argumentsSchema(argType)

	promptSchema := &PromptSchema{
		Name:        name,
		Description: &description,
		Arguments:   make([]PromptSchemaArgument, 0, inputSchema.Properties.Len()),
	}

	for pair := inputSchema.Properties.Oldest(); pair != nil; pair = pair.Next() {
		arg := PromptSchemaArgument{
			Name: pair.Key,
		}
		if descr := pair.Value.Description; descr != "" {
			arg.Description = &descr
		}
		if slices.Contains(inputSchema.Required, pair.Key) {
			required := true
			arg.Required = &required
		}
		promptSchema.Arguments = append(promptSchema.Arguments, arg)
	}
	return promptSchema
}
