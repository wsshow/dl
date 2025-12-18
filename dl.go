package dl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// 常量定义
const (
	// DefaultConcurrency 默认并发下载数
	DefaultConcurrency = 0 // 0表示使用runtime.NumCPU()
	// DefaultBaseDir 默认缓存目录
	DefaultBaseDir = "downloader_cache"
	// DefaultBufferSize 默认缓冲区大小
	DefaultBufferSize = 32 * 1024
	// RateUpdateInterval 速率更新间隔
	RateUpdateInterval = 250 * time.Millisecond
	// FilePerm 文件权限
	FilePerm = 0644
	// DirPerm 目录权限
	DirPerm = 0755
)

// 错误定义
var (
	// ErrAlreadyStopped 下载器已停止错误
	ErrAlreadyStopped = errors.New("downloader has been stopped")
	// ErrInvalidURL URL无效错误
	ErrInvalidURL = errors.New("invalid download URL")
	// ErrInvalidConcurrency 并发数无效错误
	ErrInvalidConcurrency = errors.New("concurrency must be greater than 0")
)

// selfWriter 是一个线程安全的写入器，用于跟踪下载进度和速率
type selfWriter struct {
	mu            sync.Mutex
	loaded        int64        // 已下载字节数
	total         int64        // 总字节数
	accPacketSize int64        // 累积包大小（用于速率计算）
	rate          atomic.Value // 当前下载速率（string）
	onProgress    func(loaded int64, total int64, rate string)
}

// Write 实现io.Writer接口，写入数据并更新进度
func (sw *selfWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	atomic.AddInt64(&sw.accPacketSize, int64(n))

	sw.mu.Lock()
	sw.loaded += int64(n)
	loaded := sw.loaded
	total := sw.total
	onProgress := sw.onProgress
	sw.mu.Unlock()

	if onProgress != nil {
		rate := "0.00 MB/s"
		if v := sw.rate.Load(); v != nil {
			rate = v.(string)
		}
		onProgress(loaded, total, rate)
	}
	return
}

// calcRate 持续计算并更新下载速率（每250ms更新一次）
func (sw *selfWriter) calcRate(ctx context.Context) {
	sw.rate.Store("0.00 MB/s")

	// formatRate 将字节速率格式化为易读的字符串（保留两位小数）
	formatRate := func(bytesPerSecond float64) string {
		const (
			KB = 1024.0
			MB = KB * 1024.0
			GB = MB * 1024.0
		)

		switch {
		case bytesPerSecond >= GB:
			return fmt.Sprintf("%.2f GB/s", bytesPerSecond/GB)
		case bytesPerSecond >= MB:
			return fmt.Sprintf("%.2f MB/s", bytesPerSecond/MB)
		case bytesPerSecond >= KB:
			return fmt.Sprintf("%.2f KB/s", bytesPerSecond/KB)
		default:
			return fmt.Sprintf("%.2f B/s", bytesPerSecond)
		}
	}

	ticker := time.NewTicker(RateUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 读取并重置累积包大小
			accSize := atomic.SwapInt64(&sw.accPacketSize, 0)
			// 计算每秒字节数（250ms * 4 = 1s）
			bytesPerSecond := float64(accSize) * 4.0
			sw.rate.Store(formatRate(bytesPerSecond))
		}
	}
}

// Options 下载器配置选项
type Options struct {
	// FileName 指定下载后保存的文件名（包含路径）
	FileName string
	// BaseDir 多协程下载时分片文件的缓存目录
	BaseDir string
	// Concurrency 并发下载的协程数，0表示使用CPU核心数
	Concurrency int
	// Resume 是否启用断点续传功能
	Resume bool
}

// OptionFunc 配置函数
type OptionFunc func(*Options)

// WithFileName 设置下载的文件名
func WithFileName(filename string) OptionFunc {
	return func(o *Options) {
		o.FileName = filename
	}
}

// WithBaseDir 设置多协程下载时文件的缓存目录
func WithBaseDir(basedir string) OptionFunc {
	return func(o *Options) {
		o.BaseDir = basedir
	}
}

// WithConcurrency 设置并发下载数
func WithConcurrency(concurrency int) OptionFunc {
	return func(o *Options) {
		o.Concurrency = concurrency
	}
}

// WithResume 设置是否启用下载缓存
func WithResume(resume bool) OptionFunc {
	return func(o *Options) {
		o.Resume = resume
	}
}

// Downloader 文件下载器，支持多协程并发下载和断点续传
type Downloader struct {
	url                string              // 下载URL
	concurrency        int                 // 并发数
	resume             bool                // 是否启用断点续传
	partDir            string              // 分片文件目录
	sw                 *selfWriter         // 进度跟踪器
	options            *Options            // 配置选项
	stopSignal         chan struct{}       // 停止信号
	mCancelFunc        sync.Map            // 取消函数映射表 map[string]context.CancelFunc
	onDownloadStart    func(int64, string) // 下载开始回调
	onDownloadFinished func(string)        // 下载完成回调
	onDownloadCanceled func(string)        // 下载取消回调
}

// NewDownloader 创建一个新的文件下载器实例
//
// 参数:
//
//	url - 要下载的文件URL地址
//	opts - 可选的配置函数，用于自定义下载行为
//
// 返回:
//
//	*Downloader - 配置好的下载器实例
//
// 示例:
//
//	dl := NewDownloader("https://example.com/file.zip",
//	    WithConcurrency(8),
//	    WithResume(true))
func NewDownloader(url string, opts ...OptionFunc) *Downloader {
	options := &Options{
		Concurrency: runtime.NumCPU(),
		BaseDir:     DefaultBaseDir,
		FileName:    filepath.Base(url),
		Resume:      true,
	}

	for _, opt := range opts {
		opt(options)
	}

	// 如果并发数为0，使用CPU核心数
	if options.Concurrency == 0 {
		options.Concurrency = runtime.NumCPU()
	}

	sw := &selfWriter{}
	sw.rate.Store("0.00 MB/s")

	return &Downloader{
		url:         url,
		concurrency: options.Concurrency,
		resume:      options.Resume,
		options:     options,
		sw:          sw,
		stopSignal:  make(chan struct{}),
		mCancelFunc: sync.Map{},
	}
}

// OnProgress 设置下载进度回调函数
//
// 参数:
//
//	f - 回调函数，接收已下载字节数、总字节数和当前速率
//
// 注意: 此回调会被频繁调用，应避免执行耗时操作
func (d *Downloader) OnProgress(f func(loaded int64, total int64, rate string)) {
	d.sw.mu.Lock()
	d.sw.onProgress = f
	d.sw.mu.Unlock()
}

// OnDownloadStart 设置下载开始时的回调函数
//
// 参数:
//
//	f - 回调函数，接收文件总大小和文件名
func (d *Downloader) OnDownloadStart(f func(total int64, filename string)) {
	d.onDownloadStart = f
}

// OnDownloadFinished 设置下载成功完成后的回调函数
//
// 参数:
//
//	f - 回调函数，接收已完成的文件名
func (d *Downloader) OnDownloadFinished(f func(filename string)) {
	d.onDownloadFinished = f
}

// OnDownloadCanceled 设置下载被取消时的回调函数
//
// 参数:
//
//	f - 回调函数，接收被取消的文件名
func (d *Downloader) OnDownloadCanceled(f func(filename string)) {
	d.onDownloadCanceled = f
}

// Start 开始执行下载任务
//
// 如果下载器之前被停止，会自动重新初始化
//
// 返回:
//
//	error - 下载过程中的错误，成功则返回nil
func (d *Downloader) Start() error {
	select {
	case <-d.stopSignal:
		d.init()
	default:
	}
	return d.download()
}

// Stop 停止正在进行的下载任务
//
// 此方法会取消所有正在进行的下载协程，但不会删除已下载的分片文件。
// 如果启用了断点续传，可以通过调用Resume()继续下载。
//
// 返回:
//
//	error - 如果下载器已经停止则返回ErrAlreadyStopped，否则返回nil
func (d *Downloader) Stop() error {
	select {
	case <-d.stopSignal:
		return ErrAlreadyStopped
	default:
		close(d.stopSignal)
	}

	// 取消所有正在进行的下载协程
	d.mCancelFunc.Range(func(key, value interface{}) bool {
		if cancelFunc, ok := value.(context.CancelFunc); ok {
			cancelFunc()
		}
		return true
	})
	d.mCancelFunc = sync.Map{}
	return nil
}

// Pause 暂停下载（Stop的别名）
//
// 此方法与Stop()行为完全相同。如果启用了断点续传，
// 可以通过调用Resume()从上次停止的位置继续下载。
//
// 返回:
//
//	error - 如果下载器已经停止则返回ErrAlreadyStopped，否则返回nil
func (d *Downloader) Pause() error {
	return d.Stop()
}

// Resume 恢复之前暂停的下载（Start的别名）
//
// 如果启用了断点续传，将从上次停止的位置继续下载。
//
// 返回:
//
//	error - 下载过程中的错误，成功则返回nil
func (d *Downloader) Resume() error {
	return d.Start()
}

// init 初始化下载器状态，用于重新开始下载
func (d *Downloader) init() {
	d.sw.mu.Lock()
	d.sw.loaded = 0
	d.sw.mu.Unlock()

	atomic.StoreInt64(&d.sw.accPacketSize, 0)
	d.sw.rate.Store("0.00 MB/s")
	d.stopSignal = make(chan struct{})
	d.mCancelFunc = sync.Map{}
}

// download 执行实际的下载逻辑，根据服务器支持情况选择单线程或多线程下载
func (d *Downloader) download() error {
	if d.url == "" {
		return ErrInvalidURL
	}

	// 发送HEAD请求检查服务器是否支持Range请求
	resp, err := http.Head(d.url)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}
	defer resp.Body.Close()

	// 启动速率计算协程
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.sw.calcRate(ctx)

	// 检查服务器是否支持分段下载
	if resp.StatusCode == http.StatusOK && resp.Header.Get("Accept-Ranges") == "bytes" {
		return d.multiDownload(resp.ContentLength)
	}

	return d.singleDownload()
}

// multiDownload 使用多协程并发下载文件
func (d *Downloader) multiDownload(contentLen int64) (err error) {
	if contentLen <= 0 {
		return fmt.Errorf("invalid content length: %d", contentLen)
	}

	filename := d.options.FileName

	d.sw.mu.Lock()
	d.sw.total = contentLen
	d.sw.mu.Unlock()

	if d.onDownloadStart != nil {
		d.onDownloadStart(contentLen, filename)
	}

	partSize := contentLen / int64(d.concurrency)
	partDir := d.getPartDir(filename)
	if err = os.MkdirAll(partDir, DirPerm); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	d.partDir = partDir

	var wg sync.WaitGroup

	// 启动多个协程并发下载
	rangeStart := int64(0)
	for i := 0; i < d.concurrency; i++ {
		select {
		case <-d.stopSignal:
			return nil
		default:
		}

		wg.Add(1)
		go func(i int, rangeStart int64) {
			defer wg.Done()

			// 计算当前分片的下载范围
			rangeEnd := rangeStart + partSize
			if i == d.concurrency-1 {
				rangeEnd = contentLen // 最后一个分片下载到文件末尾
			}

			// 如果启用断点续传，计算已下载的大小
			var downloaded int64
			if d.resume {
				partFileName := d.getPartFilename(filename, i)
				if content, err := os.ReadFile(partFileName); err == nil {
					downloaded = int64(len(content))
					_, _ = d.sw.Write(content)
				}
			}

			// 下载分片
			if err := d.downloadPartial(rangeStart+downloaded, rangeEnd, i); err != nil {
				// 错误已在downloadPartial中处理
				return
			}
		}(i, rangeStart)

		rangeStart += partSize
	}

	// 等待所有分片下载完成
	wg.Wait()

	// 检查是否被取消
	select {
	case <-d.stopSignal:
		if d.onDownloadCanceled != nil {
			d.onDownloadCanceled(filename)
		}
		return nil
	default:
	}

	// 合并所有分片文件
	if err = d.merge(); err != nil {
		return fmt.Errorf("failed to merge parts: %w", err)
	}

	// 删除临时目录
	if err = os.RemoveAll(partDir); err != nil {
		// 合并成功但清理失败，不应该返回错误
		// 只记录错误但继续执行
	}

	if d.onDownloadFinished != nil {
		d.onDownloadFinished(filename)
	}

	return nil
}

// downloadPartial 下载文件的指定分片
func (d *Downloader) downloadPartial(rangeStart, rangeEnd int64, i int) error {
	if rangeStart >= rangeEnd {
		return nil
	}

	url := d.url
	filename := d.options.FileName

	partFilename := d.getPartFilename(filename, i)

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())
	d.mCancelFunc.Store(partFilename, cancel)
	defer d.mCancelFunc.Delete(partFilename)

	// 创建Range请求
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 注意：Range的end是inclusive的，所以需要减1
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd-1))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download part %d: %w", i, err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("unexpected status code %d for part %d", resp.StatusCode, i)
	}

	// 打开或创建分片文件
	flags := os.O_CREATE | os.O_WRONLY
	if d.resume {
		flags |= os.O_APPEND
	}

	partFile, err := os.OpenFile(partFilename, flags, FilePerm)
	if err != nil {
		return fmt.Errorf("failed to open part file: %w", err)
	}
	defer partFile.Close()

	// 使用缓冲区复制数据
	buf := make([]byte, DefaultBufferSize)
	_, err = io.CopyBuffer(io.MultiWriter(partFile, d.sw), resp.Body, buf)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to write part %d: %w", i, err)
	}
	return nil
}

// merge 合并所有分片文件为最终文件
func (d *Downloader) merge() error {
	filename := d.options.FileName

	// 创建目标文件
	destFile, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, FilePerm)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	// 按顺序合并所有分片
	for i := 0; i < d.concurrency; i++ {
		partFileName := d.getPartFilename(filename, i)

		partFile, err := os.Open(partFileName)
		if err != nil {
			return fmt.Errorf("failed to open part %d: %w", i, err)
		}

		if _, err = io.Copy(destFile, partFile); err != nil {
			partFile.Close()
			return fmt.Errorf("failed to copy part %d: %w", i, err)
		}

		if err = partFile.Close(); err != nil {
			return fmt.Errorf("failed to close part %d: %w", i, err)
		}

		if err = os.Remove(partFileName); err != nil {
			return fmt.Errorf("failed to remove part %d: %w", i, err)
		}
	}

	return nil
}

// getPartDir 获取分片文件的存储目录
func (d *Downloader) getPartDir(filename string) string {
	return filepath.Join(d.options.BaseDir, filename)
}

// getPartFilename 获取指定分片的文件名
func (d *Downloader) getPartFilename(filename string, partNum int) string {
	return filepath.Join(d.partDir, fmt.Sprintf("%s_%d", filename, partNum))
}

// singleDownload 使用单线程下载文件（当服务器不支持Range请求时）
func (d *Downloader) singleDownload() error {
	url := d.url
	filename := d.options.FileName

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())
	d.mCancelFunc.Store(filename, cancel)
	defer d.mCancelFunc.Delete(filename)

	// 创建GET请求
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	contentLen := resp.ContentLength
	d.sw.mu.Lock()
	d.sw.total = contentLen
	d.sw.mu.Unlock()

	if d.onDownloadStart != nil {
		d.onDownloadStart(contentLen, filename)
	}

	// 创建目标文件
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, FilePerm)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	// 下载并写入文件
	buf := make([]byte, DefaultBufferSize)
	_, err = io.CopyBuffer(io.MultiWriter(f, d.sw), resp.Body, buf)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// 检查是否被取消
	select {
	case <-d.stopSignal:
		if d.onDownloadCanceled != nil {
			d.onDownloadCanceled(filename)
		}
		return nil
	default:
		if d.onDownloadFinished != nil {
			d.onDownloadFinished(filename)
		}
	}

	return nil
}
