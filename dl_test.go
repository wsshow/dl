package dl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 测试辅助函数

// createTestServer 创建一个测试用的HTTP服务器
func createTestServer(size int64, supportRange bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		if supportRange {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", size))

		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" && supportRange {
			var start, end int64
			fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)

			if start < 0 || start >= size {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}

			if end >= size || end < start {
				end = size - 1
			}

			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(data[start : end+1])
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write(data)
		}
	}))
}

// createSlowTestServer 创建一个响应缓慢的测试服务器（用于测试取消功能）
func createSlowTestServer(size int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", size))

		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusOK)

		// 每次写入少量数据并暂停
		data := make([]byte, 1024)
		remaining := size
		for remaining > 0 {
			chunkSize := int64(1024)
			if chunkSize > remaining {
				chunkSize = remaining
			}
			w.Write(data[:chunkSize])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			remaining -= chunkSize
			time.Sleep(50 * time.Millisecond)
		}
	}))
}

// cleanupTestFiles 清理测试文件
func cleanupTestFiles(paths ...string) {
	for _, path := range paths {
		os.RemoveAll(path)
	}
}

// TestNewDownloader 测试下载器的创建
func TestNewDownloader(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		opts    []OptionFunc
		wantNil bool
		checkFn func(*testing.T, *Downloader)
	}{
		{
			name:    "默认配置",
			url:     "https://example.com/file.txt",
			opts:    nil,
			wantNil: false,
			checkFn: func(t *testing.T, d *Downloader) {
				if d.url != "https://example.com/file.txt" {
					t.Errorf("url = %v, want %v", d.url, "https://example.com/file.txt")
				}
				if d.options.FileName != "file.txt" {
					t.Errorf("filename = %v, want %v", d.options.FileName, "file.txt")
				}
				if d.options.BaseDir != DefaultBaseDir {
					t.Errorf("basedir = %v, want %v", d.options.BaseDir, DefaultBaseDir)
				}
				if d.concurrency == 0 {
					t.Error("concurrency should not be 0 after initialization")
				}
				if !d.options.Resume {
					t.Error("resume should be true by default")
				}
			},
		},
		{
			name: "自定义配置",
			url:  "https://example.com/download/archive.zip",
			opts: []OptionFunc{
				WithFileName("custom.zip"),
				WithBaseDir("./test_cache"),
				WithConcurrency(4),
				WithResume(false),
			},
			wantNil: false,
			checkFn: func(t *testing.T, d *Downloader) {
				if d.options.FileName != "custom.zip" {
					t.Errorf("filename = %v, want %v", d.options.FileName, "custom.zip")
				}
				if d.options.BaseDir != "./test_cache" {
					t.Errorf("basedir = %v, want %v", d.options.BaseDir, "./test_cache")
				}
				if d.concurrency != 4 {
					t.Errorf("concurrency = %v, want %v", d.concurrency, 4)
				}
				if d.options.Resume {
					t.Error("resume should be false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDownloader(tt.url, tt.opts...)
			if (d == nil) != tt.wantNil {
				t.Errorf("NewDownloader() = %v, wantNil %v", d, tt.wantNil)
				return
			}
			if tt.checkFn != nil && d != nil {
				tt.checkFn(t, d)
			}
		})
	}
}

// TestSelfWriter 测试selfWriter的功能
func TestSelfWriter(t *testing.T) {
	t.Run("Write方法", func(t *testing.T) {
		sw := &selfWriter{}
		sw.rate.Store("0.00 MB/s")
		sw.total = 1000

		data := []byte("test data")
		n, err := sw.Write(data)

		if err != nil {
			t.Errorf("Write() error = %v", err)
		}
		if n != len(data) {
			t.Errorf("Write() n = %v, want %v", n, len(data))
		}
		if sw.loaded != int64(len(data)) {
			t.Errorf("loaded = %v, want %v", sw.loaded, len(data))
		}
	})

	t.Run("进度回调", func(t *testing.T) {
		sw := &selfWriter{}
		sw.rate.Store("0.00 MB/s")
		sw.total = 100

		var callbackLoaded, callbackTotal int64
		callbackCalled := false

		sw.onProgress = func(loaded, total int64, rate string) {
			callbackLoaded = loaded
			callbackTotal = total
			callbackCalled = true
		}

		sw.Write([]byte("12345"))

		if !callbackCalled {
			t.Error("progress callback was not called")
		}
		if callbackLoaded != 5 {
			t.Errorf("callback loaded = %v, want 5", callbackLoaded)
		}
		if callbackTotal != 100 {
			t.Errorf("callback total = %v, want 100", callbackTotal)
		}
	})

	t.Run("速率计算", func(t *testing.T) {
		sw := &selfWriter{}
		sw.rate.Store("0.00 MB/s")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go sw.calcRate(ctx)

		// 写入数据
		data := make([]byte, 1024*1024) // 1MB
		sw.Write(data)

		// 等待速率更新
		time.Sleep(300 * time.Millisecond)

		rate := sw.rate.Load()
		if rate == nil {
			t.Error("rate should not be nil")
		} else {
			rateStr := rate.(string)
			if !strings.Contains(rateStr, "/s") {
				t.Errorf("rate format incorrect: %v", rateStr)
			}
		}

		cancel()
		time.Sleep(100 * time.Millisecond)
	})
}

// TestDownloadSingle 测试单线程下载
func TestDownloadSingle(t *testing.T) {
	size := int64(1024 * 10) // 10KB
	server := createTestServer(size, false)
	defer server.Close()

	tmpFile := "test_single_download.txt"
	defer cleanupTestFiles(tmpFile)

	d := NewDownloader(server.URL,
		WithFileName(tmpFile),
		WithConcurrency(4),
	)

	var startCalled, finishCalled bool
	var progressCalled int32

	d.OnDownloadStart(func(total int64, filename string) {
		startCalled = true
		if total != size {
			t.Errorf("OnDownloadStart total = %v, want %v", total, size)
		}
		if filename != tmpFile {
			t.Errorf("OnDownloadStart filename = %v, want %v", filename, tmpFile)
		}
	})

	d.OnProgress(func(loaded, total int64, rate string) {
		atomic.AddInt32(&progressCalled, 1)
	})

	d.OnDownloadFinished(func(filename string) {
		finishCalled = true
		if filename != tmpFile {
			t.Errorf("OnDownloadFinished filename = %v, want %v", filename, tmpFile)
		}
	})

	err := d.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !startCalled {
		t.Error("OnDownloadStart was not called")
	}
	if !finishCalled {
		t.Error("OnDownloadFinished was not called")
	}
	if atomic.LoadInt32(&progressCalled) == 0 {
		t.Error("OnProgress was not called")
	}

	// 验证文件存在且大小正确
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("downloaded file does not exist: %v", err)
	}
	if info.Size() != size {
		t.Errorf("file size = %v, want %v", info.Size(), size)
	}
}

// TestDownloadMulti 测试多线程下载
func TestDownloadMulti(t *testing.T) {
	size := int64(1024 * 100) // 100KB
	server := createTestServer(size, true)
	defer server.Close()

	tmpFile := "test_multi_download.txt"
	cacheDir := "test_cache_multi"
	defer cleanupTestFiles(tmpFile, cacheDir)

	d := NewDownloader(server.URL,
		WithFileName(tmpFile),
		WithBaseDir(cacheDir),
		WithConcurrency(4),
		WithResume(true),
	)

	var finishCalled bool
	d.OnDownloadFinished(func(filename string) {
		finishCalled = true
	})

	err := d.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !finishCalled {
		t.Error("OnDownloadFinished was not called")
	}

	// 验证文件存在且大小正确
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("downloaded file does not exist: %v", err)
	}
	if info.Size() != size {
		t.Errorf("file size = %v, want %v", info.Size(), size)
	}
}

// TestDownloadStop 测试停止下载
func TestDownloadStop(t *testing.T) {
	size := int64(1024 * 100) // 100KB
	server := createSlowTestServer(size)
	defer server.Close()

	tmpFile := "test_stop_download.txt"
	cacheDir := "test_cache_stop"
	defer cleanupTestFiles(tmpFile, cacheDir)

	d := NewDownloader(server.URL,
		WithFileName(tmpFile),
		WithBaseDir(cacheDir),
		WithConcurrency(2),
	)

	var cancelCalled bool
	d.OnDownloadCanceled(func(filename string) {
		cancelCalled = true
	})

	// 在goroutine中启动下载
	errChan := make(chan error, 1)
	go func() {
		errChan <- d.Start()
	}()

	// 等待下载开始
	time.Sleep(200 * time.Millisecond)

	// 停止下载
	err := d.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// 等待下载完成
	select {
	case <-errChan:
		// 下载已停止
	case <-time.After(2 * time.Second):
		t.Error("download did not stop in time")
	}

	if !cancelCalled {
		t.Error("OnDownloadCanceled was not called")
	}

	// 再次调用Stop应该返回错误
	err = d.Stop()
	if err != ErrAlreadyStopped {
		t.Errorf("Stop() error = %v, want %v", err, ErrAlreadyStopped)
	}
}

// TestInvalidURL 测试无效URL
func TestInvalidURL(t *testing.T) {
	d := NewDownloader("",
		WithFileName("test.txt"),
	)

	err := d.Start()
	if err != ErrInvalidURL {
		t.Errorf("Start() error = %v, want %v", err, ErrInvalidURL)
	}
}

// TestDownloadError 测试下载错误处理
func TestDownloadError(t *testing.T) {
	d := NewDownloader("http://localhost:99999/nonexistent",
		WithFileName("test.txt"),
	)

	err := d.Start()
	if err == nil {
		t.Error("Start() should return error for invalid server")
	}
}

// TestConcurrentDownloads 测试并发下载
func TestConcurrentDownloads(t *testing.T) {
	size := int64(1024 * 20) // 20KB
	server := createTestServer(size, true)
	defer server.Close()

	const numDownloads = 3
	var wg sync.WaitGroup
	errors := make(chan error, numDownloads)

	for i := 0; i < numDownloads; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			tmpFile := fmt.Sprintf("test_concurrent_%d.txt", index)
			defer cleanupTestFiles(tmpFile)

			d := NewDownloader(server.URL,
				WithFileName(tmpFile),
				WithConcurrency(2),
			)

			if err := d.Start(); err != nil {
				errors <- err
				return
			}

			// 验证文件
			info, err := os.Stat(tmpFile)
			if err != nil {
				errors <- err
				return
			}
			if info.Size() != size {
				errors <- fmt.Errorf("file %s size = %v, want %v", tmpFile, info.Size(), size)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent download error: %v", err)
		}
	}
}

// TestGetPartFilename 测试分片文件名生成
func TestGetPartFilename(t *testing.T) {
	d := NewDownloader("https://example.com/file.txt",
		WithBaseDir("test_cache"),
	)
	d.partDir = filepath.Join("test_cache", "file.txt")

	filename := d.getPartFilename("file.txt", 0)
	expected := filepath.Join("test_cache", "file.txt", "file.txt_0")

	if filename != expected {
		t.Errorf("getPartFilename() = %v, want %v", filename, expected)
	}
}

// TestOptionsValidation 测试配置选项
func TestOptionsValidation(t *testing.T) {
	tests := []struct {
		name string
		opts []OptionFunc
		want func(*Downloader) bool
	}{
		{
			name: "WithFileName",
			opts: []OptionFunc{WithFileName("custom.txt")},
			want: func(d *Downloader) bool {
				return d.options.FileName == "custom.txt" && d.options.FilePath == "custom.txt"
			},
		},
		{
			name: "WithBaseDir",
			opts: []OptionFunc{WithBaseDir("custom_cache")},
			want: func(d *Downloader) bool {
				return d.options.BaseDir == "custom_cache"
			},
		},
		{
			name: "WithConcurrency",
			opts: []OptionFunc{WithConcurrency(8)},
			want: func(d *Downloader) bool {
				return d.concurrency == 8
			},
		},
		{
			name: "WithResume",
			opts: []OptionFunc{WithResume(false)},
			want: func(d *Downloader) bool {
				return d.options.Resume == false
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDownloader("https://example.com/test.txt", tt.opts...)
			if !tt.want(d) {
				t.Errorf("option %s not applied correctly", tt.name)
			}
		})
	}
}

// TestRateFormatting 测试速率格式化
func TestRateFormatting(t *testing.T) {
	sw := &selfWriter{}
	sw.rate.Store("0.00 MB/s")

	ctx, cancel := context.WithCancel(context.Background())
	go sw.calcRate(ctx)
	defer cancel()

	// 写入不同大小的数据测试不同单位
	testCases := []struct {
		size     int64
		minDelay time.Duration
	}{
		{100, 300 * time.Millisecond},
		{10 * 1024, 300 * time.Millisecond},
		{1024 * 1024, 300 * time.Millisecond},
	}

	for _, tc := range testCases {
		atomic.StoreInt64(&sw.accPacketSize, 0)
		data := make([]byte, tc.size)
		sw.Write(data)
		time.Sleep(tc.minDelay)

		rate := sw.rate.Load()
		if rate == nil {
			t.Error("rate should not be nil")
			continue
		}

		rateStr := rate.(string)
		if !strings.Contains(rateStr, "/s") {
			t.Errorf("rate format incorrect: %v", rateStr)
		}

		// 验证包含小数点
		if !strings.Contains(rateStr, ".") {
			t.Errorf("rate should contain decimal point: %v", rateStr)
		}
	}
}

// TestContextCancellation 测试上下文取消
func TestContextCancellation(t *testing.T) {
	sw := &selfWriter{}
	sw.rate.Store("0.00 MB/s")

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan bool)
	go func() {
		sw.calcRate(ctx)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// calcRate 正确响应了取消
	case <-time.After(1 * time.Second):
		t.Error("calcRate did not stop after context cancellation")
	}
}

// BenchmarkDownload 基准测试
func BenchmarkDownload(b *testing.B) {
	size := int64(1024 * 100) // 100KB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer server.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tmpFile := fmt.Sprintf("bench_download_%d.txt", i)
		d := NewDownloader(server.URL,
			WithFileName(tmpFile),
			WithConcurrency(4),
		)

		if err := d.Start(); err != nil {
			b.Fatalf("Start() error = %v", err)
		}

		os.Remove(tmpFile)
	}
}

// BenchmarkConcurrentWrites 测试并发写入性能
func BenchmarkConcurrentWrites(b *testing.B) {
	sw := &selfWriter{}
	sw.rate.Store("0.00 MB/s")
	sw.total = int64(b.N * 1024)

	data := make([]byte, 1024)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sw.Write(data)
		}
	})
}
