package auth

import (
	"context"
	"net/http"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type connectivityExecutorStub struct {
	executions int
	disabled   bool
}

func (s *connectivityExecutorStub) Identifier() string { return "connectivity-provider" }

func (s *connectivityExecutorStub) Execute(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	s.executions++
	s.disabled = auth.Disabled || auth.Status == StatusDisabled
	return cliproxyexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
}

func (s *connectivityExecutorStub) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (s *connectivityExecutorStub) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (s *connectivityExecutorStub) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (s *connectivityExecutorStub) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestConnectivityExecutionCanPinDisabledAuthWithoutActivatingIt(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	executor := &connectivityExecutorStub{}
	manager.RegisterExecutor(executor)
	registered, err := manager.Register(context.Background(), &Auth{
		ID: "disabled-auth", Provider: executor.Identifier(), Disabled: true, Status: StatusDisabled,
	})
	if err != nil {
		t.Fatalf("注册停用认证失败：%v", err)
	}

	_, err = manager.Execute(context.Background(), []string{executor.Identifier()}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.PinnedAuthMetadataKey:       registered.ID,
			cliproxyexecutor.ConnectivityTestMetadataKey: true,
		},
	})
	if err != nil {
		t.Fatalf("指定停用认证测试失败：%v", err)
	}
	if executor.executions != 1 || executor.disabled {
		t.Fatalf("执行次数=%d，执行时停用=%v", executor.executions, executor.disabled)
	}
	stored, ok := manager.GetByID(registered.ID)
	if !ok || !stored.Disabled || stored.Status != StatusDisabled {
		t.Fatalf("测试后认证状态被修改：%#v", stored)
	}

	_, err = manager.Execute(context.Background(), []string{executor.Identifier()}, cliproxyexecutor.Request{Model: "test-model"}, cliproxyexecutor.Options{
		Metadata: map[string]any{cliproxyexecutor.PinnedAuthMetadataKey: registered.ID},
	})
	if err == nil {
		t.Fatal("普通请求不应选中停用认证")
	}
	if executor.executions != 1 {
		t.Fatalf("普通请求错误执行了停用认证，次数=%d", executor.executions)
	}
}
