package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"

	lapuatool "github.com/infinmalum/Lapua/tool"
)

// guardedWebFetch wraps the Lapua WebFetch tool with an SSRF guard. The SDK
// implementation fetches whatever URL the model supplies straight from the
// gateway process; on a cloud host that exposes loopback services, cloud
// metadata endpoints, and the admin surface. The wrapper resolves the target
// host and rejects any address that is not public before delegating.
//
// The check happens before the SDK's own dial, so a rebinding DNS answer
// could still reach a private address between validation and connection;
// closing that window needs a custom dialer inside the SDK.
type guardedWebFetch struct {
	lapuatool.CoreTool
}

func newGuardedWebFetch(core lapuatool.CoreTool) lapuatool.CoreTool {
	return &guardedWebFetch{CoreTool: core}
}

func (g *guardedWebFetch) Call(ctx context.Context, input json.RawMessage, tc *lapuatool.ToolContext) (lapuatool.Result, error) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return lapuatool.Errorf("Error: invalid arguments: " + err.Error()), nil
	}
	if err := validatePublicURL(ctx, in.URL); err != nil {
		return lapuatool.Errorf("Error: " + err.Error()), nil
	}
	return g.CoreTool.Call(ctx, input, tc)
}

// validatePublicURL rejects anything that is not a plain http(s) URL whose
// host resolves only to public unicast addresses.
func validatePublicURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("无效的 URL: %v", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("仅支持 http(s) URL")
	}
	if parsed.User != nil {
		return fmt.Errorf("URL 不允许携带用户信息")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL 缺少主机名")
	}
	if ip := net.ParseIP(host); ip != nil {
		if forbiddenIP(ip) {
			return fmt.Errorf("拒绝访问非公网地址")
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("域名解析失败: %v", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("域名解析失败: 无可用地址")
	}
	for _, addr := range ips {
		if forbiddenIP(addr.IP) {
			return fmt.Errorf("拒绝访问解析到非公网地址的域名")
		}
	}
	return nil
}

// forbiddenIP reports addresses a model-supplied fetch must not target:
// unspecified, loopback, private, link-local, multicast, and CGNAT ranges.
func forbiddenIP(ip net.IP) bool {
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// Shared address space (RFC 6598, 100.64/10) and the reserved 0/8.
		if v4[0] == 100 && v4[1] == 64 {
			return true
		}
		if v4[0] == 0 {
			return true
		}
	}
	// Unique local addresses (fc00::/7) — IsPrivate covers fd00::/8 only.
	if len(ip) == 16 && ip[0]&0xfe == 0xfc {
		return true
	}
	return false
}
