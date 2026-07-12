package repository

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// httpClientSink 用于防止编译器优化掉基准测试中的赋值操作
// 这是 Go 基准测试的常见模式，确保测试结果准确
var httpClientSink *http.Client

// BenchmarkHTTPUpstreamClient 对比构建 Transport 与真实缓存命中路径。
//
// 测试目的：
// - 直连是默认路径，缓存命中不应产生临时 key 分配
// - 代理路径仍需解析和规范化 URL，单独报告其成本
func BenchmarkHTTPUpstreamClient(b *testing.B) {
	// 创建测试配置
	cfg := &config.Config{
		Gateway: config.GatewayConfig{ResponseHeaderTimeout: 300},
	}
	upstream := NewHTTPUpstream(cfg)
	if closer, ok := upstream.(interface{ Close() }); ok {
		b.Cleanup(closer.Close)
	}
	svc, ok := upstream.(*httpUpstreamService)
	if !ok {
		b.Fatalf("类型断言失败，无法获取 httpUpstreamService")
	}

	b.ReportAllocs() // 报告内存分配统计

	b.Run("新建Transport", func(b *testing.B) {
		proxyURL := "http://127.0.0.1:8080"
		parsedProxy, err := url.Parse(proxyURL)
		if err != nil {
			b.Fatalf("解析代理地址失败: %v", err)
		}
		settings := defaultPoolSettings(cfg)
		for i := 0; i < b.N; i++ {
			// 每次迭代都创建新客户端，包含 Transport 分配
			transport, err := buildUpstreamTransport(settings, parsedProxy, upstreamProtocolModeDefault)
			if err != nil {
				b.Fatalf("创建 Transport 失败: %v", err)
			}
			httpClientSink = &http.Client{
				Transport: transport,
			}
		}
	})

	for _, tc := range []struct {
		name     string
		proxyURL string
	}{
		{name: "直连缓存命中", proxyURL: ""},
		{name: "代理缓存命中", proxyURL: "http://127.0.0.1:8080"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			entry, err := svc.getOrCreateClient(tc.proxyURL, 1, 1)
			if err != nil {
				b.Fatalf("getOrCreateClient warmup: %v", err)
			}
			client := entry.client
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cached, err := svc.getOrCreateClient(tc.proxyURL, 1, 1)
				if err != nil {
					b.Fatalf("getOrCreateClient cache hit: %v", err)
				}
				if cached.client != client {
					b.Fatal("cache hit returned a different client")
				}
				httpClientSink = cached.client
			}
		})
	}
}
