package common

import "testing"

func TestVerificationCodeSingleUse(t *testing.T) {
	RegisterVerificationCodeWithKey("single@example.com", "123456", EmailVerificationPurpose)
	if !VerifyCodeWithKey("single@example.com", "123456", EmailVerificationPurpose) {
		t.Fatal("首次校验应通过")
	}
	if VerifyCodeWithKey("single@example.com", "123456", EmailVerificationPurpose) {
		t.Fatal("校验成功后验证码应被消耗，不能重放")
	}
}

func TestVerificationCodeWrongAttemptDoesNotBurn(t *testing.T) {
	RegisterVerificationCodeWithKey("wrong@example.com", "123456", EmailVerificationPurpose)
	if VerifyCodeWithKey("wrong@example.com", "000000", EmailVerificationPurpose) {
		t.Fatal("错误验证码不应通过")
	}
	if !VerifyCodeWithKey("wrong@example.com", "123456", EmailVerificationPurpose) {
		t.Fatal("输错一次后正确验证码仍应可用")
	}
}

func TestGenerateVerificationCodeClampsLength(t *testing.T) {
	if code := GenerateVerificationCode(100); len(code) != 32 {
		t.Fatalf("超长请求应截断到 32，实际 %d", len(code))
	}
}
