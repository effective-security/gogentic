// Code generated by MockGen. DO NOT EDIT.
// Source: tools.go
//
// Generated by this command:
//
//	mockgen -source=tools.go -destination=../mocks/mocktools/assistants_mock.gen.go -package mocktools
//

// Package mocktools is a generated GoMock package.
package mocktools

import (
	context "context"
	reflect "reflect"

	tools "github.com/effective-security/gogentic/tools"
	jsonschema "github.com/invopop/jsonschema"
	mcp_golang "github.com/metoro-io/mcp-golang"
	gomock "go.uber.org/mock/gomock"
)

// MockMcpServerRegistrator is a mock of McpServerRegistrator interface.
type MockMcpServerRegistrator struct {
	ctrl     *gomock.Controller
	recorder *MockMcpServerRegistratorMockRecorder
	isgomock struct{}
}

// MockMcpServerRegistratorMockRecorder is the mock recorder for MockMcpServerRegistrator.
type MockMcpServerRegistratorMockRecorder struct {
	mock *MockMcpServerRegistrator
}

// NewMockMcpServerRegistrator creates a new mock instance.
func NewMockMcpServerRegistrator(ctrl *gomock.Controller) *MockMcpServerRegistrator {
	mock := &MockMcpServerRegistrator{ctrl: ctrl}
	mock.recorder = &MockMcpServerRegistratorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockMcpServerRegistrator) EXPECT() *MockMcpServerRegistratorMockRecorder {
	return m.recorder
}

// RegisterTool mocks base method.
func (m *MockMcpServerRegistrator) RegisterTool(name, description string, handler any) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterTool", name, description, handler)
	ret0, _ := ret[0].(error)
	return ret0
}

// RegisterTool indicates an expected call of RegisterTool.
func (mr *MockMcpServerRegistratorMockRecorder) RegisterTool(name, description, handler any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterTool", reflect.TypeOf((*MockMcpServerRegistrator)(nil).RegisterTool), name, description, handler)
}

// MockITool is a mock of ITool interface.
type MockITool struct {
	ctrl     *gomock.Controller
	recorder *MockIToolMockRecorder
	isgomock struct{}
}

// MockIToolMockRecorder is the mock recorder for MockITool.
type MockIToolMockRecorder struct {
	mock *MockITool
}

// NewMockITool creates a new mock instance.
func NewMockITool(ctrl *gomock.Controller) *MockITool {
	mock := &MockITool{ctrl: ctrl}
	mock.recorder = &MockIToolMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockITool) EXPECT() *MockIToolMockRecorder {
	return m.recorder
}

// Call mocks base method.
func (m *MockITool) Call(arg0 context.Context, arg1 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Call", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Call indicates an expected call of Call.
func (mr *MockIToolMockRecorder) Call(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Call", reflect.TypeOf((*MockITool)(nil).Call), arg0, arg1)
}

// Description mocks base method.
func (m *MockITool) Description() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Description")
	ret0, _ := ret[0].(string)
	return ret0
}

// Description indicates an expected call of Description.
func (mr *MockIToolMockRecorder) Description() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Description", reflect.TypeOf((*MockITool)(nil).Description))
}

// Name mocks base method.
func (m *MockITool) Name() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Name")
	ret0, _ := ret[0].(string)
	return ret0
}

// Name indicates an expected call of Name.
func (mr *MockIToolMockRecorder) Name() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Name", reflect.TypeOf((*MockITool)(nil).Name))
}

// Parameters mocks base method.
func (m *MockITool) Parameters() *jsonschema.Schema {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Parameters")
	ret0, _ := ret[0].(*jsonschema.Schema)
	return ret0
}

// Parameters indicates an expected call of Parameters.
func (mr *MockIToolMockRecorder) Parameters() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Parameters", reflect.TypeOf((*MockITool)(nil).Parameters))
}

// MockCallback is a mock of Callback interface.
type MockCallback struct {
	ctrl     *gomock.Controller
	recorder *MockCallbackMockRecorder
	isgomock struct{}
}

// MockCallbackMockRecorder is the mock recorder for MockCallback.
type MockCallbackMockRecorder struct {
	mock *MockCallback
}

// NewMockCallback creates a new mock instance.
func NewMockCallback(ctrl *gomock.Controller) *MockCallback {
	mock := &MockCallback{ctrl: ctrl}
	mock.recorder = &MockCallbackMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockCallback) EXPECT() *MockCallbackMockRecorder {
	return m.recorder
}

// OnToolEnd mocks base method.
func (m *MockCallback) OnToolEnd(ctx context.Context, tool tools.ITool, assistantName, input, output string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnToolEnd", ctx, tool, assistantName, input, output)
}

// OnToolEnd indicates an expected call of OnToolEnd.
func (mr *MockCallbackMockRecorder) OnToolEnd(ctx, tool, assistantName, input, output any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnToolEnd", reflect.TypeOf((*MockCallback)(nil).OnToolEnd), ctx, tool, assistantName, input, output)
}

// OnToolError mocks base method.
func (m *MockCallback) OnToolError(ctx context.Context, tool tools.ITool, assistantName, input string, err error) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnToolError", ctx, tool, assistantName, input, err)
}

// OnToolError indicates an expected call of OnToolError.
func (mr *MockCallbackMockRecorder) OnToolError(ctx, tool, assistantName, input, err any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnToolError", reflect.TypeOf((*MockCallback)(nil).OnToolError), ctx, tool, assistantName, input, err)
}

// OnToolStart mocks base method.
func (m *MockCallback) OnToolStart(ctx context.Context, tool tools.ITool, assistantName, input string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnToolStart", ctx, tool, assistantName, input)
}

// OnToolStart indicates an expected call of OnToolStart.
func (mr *MockCallbackMockRecorder) OnToolStart(ctx, tool, assistantName, input any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnToolStart", reflect.TypeOf((*MockCallback)(nil).OnToolStart), ctx, tool, assistantName, input)
}

// MockTool is a mock of Tool interface.
type MockTool[I any, O any] struct {
	ctrl     *gomock.Controller
	recorder *MockToolMockRecorder[I, O]
	isgomock struct{}
}

// MockToolMockRecorder is the mock recorder for MockTool.
type MockToolMockRecorder[I any, O any] struct {
	mock *MockTool[I, O]
}

// NewMockTool creates a new mock instance.
func NewMockTool[I any, O any](ctrl *gomock.Controller) *MockTool[I, O] {
	mock := &MockTool[I, O]{ctrl: ctrl}
	mock.recorder = &MockToolMockRecorder[I, O]{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTool[I, O]) EXPECT() *MockToolMockRecorder[I, O] {
	return m.recorder
}

// Call mocks base method.
func (m *MockTool[I, O]) Call(arg0 context.Context, arg1 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Call", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Call indicates an expected call of Call.
func (mr *MockToolMockRecorder[I, O]) Call(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Call", reflect.TypeOf((*MockTool[I, O])(nil).Call), arg0, arg1)
}

// Description mocks base method.
func (m *MockTool[I, O]) Description() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Description")
	ret0, _ := ret[0].(string)
	return ret0
}

// Description indicates an expected call of Description.
func (mr *MockToolMockRecorder[I, O]) Description() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Description", reflect.TypeOf((*MockTool[I, O])(nil).Description))
}

// Name mocks base method.
func (m *MockTool[I, O]) Name() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Name")
	ret0, _ := ret[0].(string)
	return ret0
}

// Name indicates an expected call of Name.
func (mr *MockToolMockRecorder[I, O]) Name() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Name", reflect.TypeOf((*MockTool[I, O])(nil).Name))
}

// Parameters mocks base method.
func (m *MockTool[I, O]) Parameters() *jsonschema.Schema {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Parameters")
	ret0, _ := ret[0].(*jsonschema.Schema)
	return ret0
}

// Parameters indicates an expected call of Parameters.
func (mr *MockToolMockRecorder[I, O]) Parameters() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Parameters", reflect.TypeOf((*MockTool[I, O])(nil).Parameters))
}

// Run mocks base method.
func (m *MockTool[I, O]) Run(arg0 context.Context, arg1 *I) (*O, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Run", arg0, arg1)
	ret0, _ := ret[0].(*O)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Run indicates an expected call of Run.
func (mr *MockToolMockRecorder[I, O]) Run(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*MockTool[I, O])(nil).Run), arg0, arg1)
}

// MockIMCPTool is a mock of IMCPTool interface.
type MockIMCPTool struct {
	ctrl     *gomock.Controller
	recorder *MockIMCPToolMockRecorder
	isgomock struct{}
}

// MockIMCPToolMockRecorder is the mock recorder for MockIMCPTool.
type MockIMCPToolMockRecorder struct {
	mock *MockIMCPTool
}

// NewMockIMCPTool creates a new mock instance.
func NewMockIMCPTool(ctrl *gomock.Controller) *MockIMCPTool {
	mock := &MockIMCPTool{ctrl: ctrl}
	mock.recorder = &MockIMCPToolMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockIMCPTool) EXPECT() *MockIMCPToolMockRecorder {
	return m.recorder
}

// Call mocks base method.
func (m *MockIMCPTool) Call(arg0 context.Context, arg1 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Call", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Call indicates an expected call of Call.
func (mr *MockIMCPToolMockRecorder) Call(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Call", reflect.TypeOf((*MockIMCPTool)(nil).Call), arg0, arg1)
}

// Description mocks base method.
func (m *MockIMCPTool) Description() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Description")
	ret0, _ := ret[0].(string)
	return ret0
}

// Description indicates an expected call of Description.
func (mr *MockIMCPToolMockRecorder) Description() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Description", reflect.TypeOf((*MockIMCPTool)(nil).Description))
}

// Name mocks base method.
func (m *MockIMCPTool) Name() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Name")
	ret0, _ := ret[0].(string)
	return ret0
}

// Name indicates an expected call of Name.
func (mr *MockIMCPToolMockRecorder) Name() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Name", reflect.TypeOf((*MockIMCPTool)(nil).Name))
}

// Parameters mocks base method.
func (m *MockIMCPTool) Parameters() *jsonschema.Schema {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Parameters")
	ret0, _ := ret[0].(*jsonschema.Schema)
	return ret0
}

// Parameters indicates an expected call of Parameters.
func (mr *MockIMCPToolMockRecorder) Parameters() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Parameters", reflect.TypeOf((*MockIMCPTool)(nil).Parameters))
}

// RegisterMCP mocks base method.
func (m *MockIMCPTool) RegisterMCP(registrator tools.McpServerRegistrator) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterMCP", registrator)
	ret0, _ := ret[0].(error)
	return ret0
}

// RegisterMCP indicates an expected call of RegisterMCP.
func (mr *MockIMCPToolMockRecorder) RegisterMCP(registrator any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterMCP", reflect.TypeOf((*MockIMCPTool)(nil).RegisterMCP), registrator)
}

// MockMCPTool is a mock of MCPTool interface.
type MockMCPTool[I any] struct {
	ctrl     *gomock.Controller
	recorder *MockMCPToolMockRecorder[I]
	isgomock struct{}
}

// MockMCPToolMockRecorder is the mock recorder for MockMCPTool.
type MockMCPToolMockRecorder[I any] struct {
	mock *MockMCPTool[I]
}

// NewMockMCPTool creates a new mock instance.
func NewMockMCPTool[I any](ctrl *gomock.Controller) *MockMCPTool[I] {
	mock := &MockMCPTool[I]{ctrl: ctrl}
	mock.recorder = &MockMCPToolMockRecorder[I]{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockMCPTool[I]) EXPECT() *MockMCPToolMockRecorder[I] {
	return m.recorder
}

// Call mocks base method.
func (m *MockMCPTool[I]) Call(arg0 context.Context, arg1 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Call", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Call indicates an expected call of Call.
func (mr *MockMCPToolMockRecorder[I]) Call(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Call", reflect.TypeOf((*MockMCPTool[I])(nil).Call), arg0, arg1)
}

// Description mocks base method.
func (m *MockMCPTool[I]) Description() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Description")
	ret0, _ := ret[0].(string)
	return ret0
}

// Description indicates an expected call of Description.
func (mr *MockMCPToolMockRecorder[I]) Description() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Description", reflect.TypeOf((*MockMCPTool[I])(nil).Description))
}

// Name mocks base method.
func (m *MockMCPTool[I]) Name() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Name")
	ret0, _ := ret[0].(string)
	return ret0
}

// Name indicates an expected call of Name.
func (mr *MockMCPToolMockRecorder[I]) Name() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Name", reflect.TypeOf((*MockMCPTool[I])(nil).Name))
}

// Parameters mocks base method.
func (m *MockMCPTool[I]) Parameters() *jsonschema.Schema {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Parameters")
	ret0, _ := ret[0].(*jsonschema.Schema)
	return ret0
}

// Parameters indicates an expected call of Parameters.
func (mr *MockMCPToolMockRecorder[I]) Parameters() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Parameters", reflect.TypeOf((*MockMCPTool[I])(nil).Parameters))
}

// RegisterMCP mocks base method.
func (m *MockMCPTool[I]) RegisterMCP(registrator tools.McpServerRegistrator) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterMCP", registrator)
	ret0, _ := ret[0].(error)
	return ret0
}

// RegisterMCP indicates an expected call of RegisterMCP.
func (mr *MockMCPToolMockRecorder[I]) RegisterMCP(registrator any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterMCP", reflect.TypeOf((*MockMCPTool[I])(nil).RegisterMCP), registrator)
}

// RunMCP mocks base method.
func (m *MockMCPTool[I]) RunMCP(arg0 context.Context, arg1 *I) (*mcp_golang.ToolResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RunMCP", arg0, arg1)
	ret0, _ := ret[0].(*mcp_golang.ToolResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RunMCP indicates an expected call of RunMCP.
func (mr *MockMCPToolMockRecorder[I]) RunMCP(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RunMCP", reflect.TypeOf((*MockMCPTool[I])(nil).RunMCP), arg0, arg1)
}
