package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bizerr "micro-scaffold/pkg/errors"
)

// ErrSDKDenied 权限 SDK 返回的无权限（映射 COM4002）。
var ErrSDKDenied = errors.New("auth: sdk permission denied")

// SDKTokenClient 由权限 SDK 实现（业务仓注入）。
type SDKTokenClient interface {
	Verify(ctx context.Context, token string) (subject string, roles []string, err error)
}

// SDKPermClient 资源权限校验。
type SDKPermClient interface {
	Check(ctx context.Context, subject, resource, action string) error
}

// SDKAuthenticator Mode=sdk 身份鉴权。
type SDKAuthenticator struct {
	Client SDKTokenClient
}

func (a SDKAuthenticator) Authenticate(ctx context.Context, token string) (*Principal, error) {
	if a.Client == nil {
		return nil, fmt.Errorf("auth: sdk authenticator: nil client")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, bizerr.NewFromContext(ctx, bizerr.CodeUnauthorized, nil)
	}
	subject, roles, err := a.Client.Verify(ctx, token)
	if err != nil {
		return nil, bizerr.NewFromContext(ctx, bizerr.CodeUnauthorized, nil)
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, bizerr.NewFromContext(ctx, bizerr.CodeUnauthorized, nil)
	}
	if len(roles) == 0 {
		roles = []string{"authenticated"}
	}
	return &Principal{UserID: subject, Roles: append([]string(nil), roles...)}, nil
}

// SDKAuthorizer Mode=sdk 资源鉴权。
type SDKAuthorizer struct {
	Client SDKPermClient
}

func (a SDKAuthorizer) Authorize(ctx context.Context, p *Principal, resource, action string) error {
	if a.Client == nil {
		return nil
	}
	if p == nil || strings.TrimSpace(p.UserID) == "" {
		return bizerr.NewFromContext(ctx, bizerr.CodeUnauthorized, nil)
	}
	if err := a.Client.Check(ctx, p.UserID, resource, action); err != nil {
		return bizerr.NewFromContext(ctx, bizerr.CodeForbidden, nil)
	}
	return nil
}

// AuthSDKBridge 把外部 SDK 适配为 Token/Perm 客户端。
type AuthSDKBridge struct {
	VerifyFn func(ctx context.Context, token string) (subject string, roles []string, err error)
	CheckFn  func(ctx context.Context, subject, resource, action string) error
}

func (b AuthSDKBridge) Verify(ctx context.Context, token string) (string, []string, error) {
	if b.VerifyFn == nil {
		return "", nil, fmt.Errorf("auth: nil VerifyFn")
	}
	return b.VerifyFn(ctx, token)
}

func (b AuthSDKBridge) Check(ctx context.Context, subject, resource, action string) error {
	if b.CheckFn == nil {
		return nil
	}
	return b.CheckFn(ctx, subject, resource, action)
}
