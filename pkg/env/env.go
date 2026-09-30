package env

import (
	"fmt"
	"os"
	"strings"
)

// ResolveAppEnv 规范化并校验 APP_ENV。
// 允许：空/development→dev，production→prod，以及 dev|test|prod。
// 未知值 fail-closed，避免拼错时静默落到 default.yaml 的 Auth.Mode=dev。
func ResolveAppEnv() (string, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	switch raw {
	case "", "development":
		return "dev", nil
	case "production":
		return "prod", nil
	case "dev", "test", "prod":
		return raw, nil
	default:
		return "", fmt.Errorf("unsupported APP_ENV=%q (want dev|test|prod|development|production)", raw)
	}
}

// AppEnv 返回规范化环境名。非法 APP_ENV 时返回空串（禁止静默 "dev"）。
// 启动路径必须先调用 ResolveAppEnv 并处理 error。
func AppEnv() string {
	e, err := ResolveAppEnv()
	if err != nil {
		return ""
	}
	return e
}

// IsProd 是否生产环境。非法 APP_ENV 时 fail-closed 视为 prod（避免拼错削弱门禁）。
func IsProd() bool {
	e, err := ResolveAppEnv()
	if err != nil {
		return true
	}
	return e == "prod"
}

// IsNonDev 是否非本地开发（test|prod）。非法 APP_ENV 时 fail-closed 视为 non-dev。
func IsNonDev() bool {
	e, err := ResolveAppEnv()
	if err != nil {
		return true
	}
	return e == "test" || e == "prod"
}
