package logger

// CurrentRedactorForTest 导出当前脱敏器供单测断言。
func CurrentRedactorForTest() Redactor {
	return currentRedactor()
}
