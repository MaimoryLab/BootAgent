package desktopapp

import "testing"

func TestDistinguishedNameAttributeKeepsQuotedCommas(t *testing.T) {
	for _, tc := range []struct {
		subject, key, want string
	}{
		{`CN="Hangzhou DeepSeek Artificial Intelligence Co., Ltd.", O="Hangzhou DeepSeek Artificial Intelligence Co., Ltd.", L=Hangzhou, C=CN`, "O", "Hangzhou DeepSeek Artificial Intelligence Co., Ltd."},
		{`CN=Microsoft Corporation, O=Microsoft Corporation, L=Redmond`, "O", "Microsoft Corporation"},
		{`CN=北京智谱华章科技股份有限公司, O=北京智谱华章科技股份有限公司`, "o", "北京智谱华章科技股份有限公司"},
		{`CN="Say ""Hi"", Inc.", O="Say ""Hi"", Inc."`, "O", `Say "Hi", Inc.`},
		{`CN=Tencent, OU=Dev, C=CN`, "O", ""},
		{`OID.2.5.4.15=Private Organization, O=Example`, "O", "Example"},
		{``, "O", ""},
	} {
		if got := distinguishedNameAttribute(tc.subject, tc.key); got != tc.want {
			t.Errorf("distinguishedNameAttribute(%q, %q) = %q, want %q", tc.subject, tc.key, got, tc.want)
		}
	}
}
