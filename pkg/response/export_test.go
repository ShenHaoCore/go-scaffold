package response

// SetReasonTrailerMismatchWarnForTest 仅供单测替换不一致告警钩子；返回 restore。
func SetReasonTrailerMismatchWarnForTest(fn func(reason, trailer string)) (restore func()) {
	prev := reasonTrailerMismatchWarn
	if fn == nil {
		fn = func(string, string) {}
	}
	reasonTrailerMismatchWarn = fn
	return func() { reasonTrailerMismatchWarn = prev }
}
