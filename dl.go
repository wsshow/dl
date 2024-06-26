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
	"strconv"
	"sync"
	"time"
)

type selfWriter struct {
	mu            *sync.Mutex
	loaded        int
	total         int
	accPacketSize int64
	rate          string
	onProgress    func(loaded int, total int, rate string)
}

func (sw *selfWriter) Write(p []byte) (n int, err error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	n = len(p)
	sw.accPacketSize += int64(n)
	sw.loaded += n
	if sw.onProgress != nil {
		sw.onProgress(sw.loaded, sw.total, sw.rate)
	}
	return
}

func (sw *selfWriter) calcRate(ctx context.Context) {
	sw.rate = "0MB/s"
	suitableDisplaySize := func(size int64) string {
		if size > (1 << 30) {
			return strconv.FormatInt(size>>30, 10) + "GB"
		} else if size > (1 << 20) {
			return strconv.FormatInt(size>>20, 10) + "MB"
		} else if size > (1 << 10) {
			return strconv.FormatInt(size>>10, 10) + "KB"
		} else {
			return strconv.FormatInt(size, 10) + "B"
		}
	}
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sw.rate = fmt.Sprintf("%s/s", suitableDisplaySize(sw.accPacketSize<<2))
			sw.accPacketSize = 0
		}
	}
}

// Options 下载器配置
type Options struct {
	FileName    string // 下载文件名
	BaseDir     string // 多协程下载时文件的缓存目录
	Concurrency int    // 并发下载数
	Resume      bool   // 是否启用下载缓存，若下载中断可在缓存处恢复下载
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

// Downloader 下载器
type Downloader struct {
	url                string
	concurrency        int
	resume             bool
	partDir            string
	sw                 *selfWriter
	options            *Options
	stopSignal         chan struct{}
	mCancelFunc        sync.Map
	onDownloadStart    func(int, string)
	onDownloadFinished func(string)
	onDownloadCanceled func(string)
}

// NewDownloader 创建下载器
// url 下载地址
// opts 下载配置
func NewDownloader(url string, opts ...OptionFunc) *Downloader {

	options := &Options{
		Concurrency: runtime.NumCPU(),
		BaseDir:     "downloader_cache",
		FileName:    filepath.Base(url),
		Resume:      true,
	}

	for _, opt := range opts {
		opt(options)
	}

	return &Downloader{
		url:         url,
		concurrency: options.Concurrency,
		resume:      options.Resume,
		options:     options,
		sw:          &selfWriter{mu: &sync.Mutex{}},
		stopSignal:  make(chan struct{}),
		mCancelFunc: sync.Map{},
	}
}

// OnProgress 设置下载进度回调
func (d *Downloader) OnProgress(f func(loaded int, total int, rate string)) {
	d.sw.onProgress = f
}

// OnDownloadStart 设置下载前的回调
func (d *Downloader) OnDownloadStart(f func(total int, filename string)) {
	d.onDownloadStart = f
}

// OnDownloadAfter 设置完成后的回调
func (d *Downloader) OnDownloadFinished(f func(filename string)) {
	d.onDownloadFinished = f
}

// OnDownloadCanceled 设置下载取消的回调
func (d *Downloader) OnDownloadCanceled(f func(filename string)) {
	d.onDownloadCanceled = f
}

// Start 开始下载
func (d *Downloader) Start() error {
	select {
	case <-d.stopSignal:
		d.init()
	default:
	}
	err := d.download()
	return err
}

// Stop 停止下载
func (d *Downloader) Stop() error {
	select {
	case <-d.stopSignal:
		return errors.New("downloader has been stopped")
	default:
		close(d.stopSignal)
	}
	d.mCancelFunc.Range(func(key, value interface{}) bool {
		cancelFunc := value.(context.CancelFunc)
		cancelFunc()
		return true
	})
	d.mCancelFunc = sync.Map{}
	return nil
}

// Pause 暂停下载
func (d *Downloader) Pause() error {
	err := d.Stop()
	return err
}

// Resume 恢复下载
func (d *Downloader) Resume() error {
	err := d.Start()
	return err
}

func (d *Downloader) init() {
	d.sw.loaded = 0
	d.stopSignal = make(chan struct{})
	d.mCancelFunc = sync.Map{}
}

func (d *Downloader) download() error {

	resp, err := http.Head(d.url)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.sw.calcRate(ctx)

	if resp.StatusCode == http.StatusOK && resp.Header.Get("Accept-Ranges") == "bytes" {
		return d.multiDownload(int(resp.ContentLength))
	}

	return d.singleDownload()
}

func (d *Downloader) multiDownload(contentLen int) (err error) {

	filename := d.options.FileName

	d.sw.total = contentLen

	if d.onDownloadStart != nil {
		d.onDownloadStart(contentLen, filename)
	}

	partSize := contentLen / d.concurrency
	partDir := d.getPartDir(filename)
	err = os.MkdirAll(partDir, 0777)
	if err != nil {
		return
	}

	d.partDir = partDir

	var wg sync.WaitGroup
	wg.Add(d.concurrency)

	rangeStart := 0

	for i := 0; i < d.concurrency; i++ {

		select {
		case <-d.stopSignal:
			return
		default:
		}

		if err != nil {
			return
		}

		go func(i, rangeStart int) {
			defer wg.Done()

			rangeEnd := rangeStart + partSize
			if i == d.concurrency-1 {
				rangeEnd = contentLen
			}

			downloaded := 0
			if d.resume {
				partFileName := d.getPartFilename(filename, i)
				content, err := os.ReadFile(partFileName)
				if err == nil {
					downloaded, _ = d.sw.Write(content)
				}
			}

			err = d.downloadPartial(rangeStart+downloaded, rangeEnd, i)
			if err != nil {
				return
			}

		}(i, rangeStart)

		rangeStart += partSize + 1
	}

	wg.Wait()

	select {
	case <-d.stopSignal:
		if d.onDownloadCanceled != nil {
			d.onDownloadCanceled(filename)
		}
		return err
	default:
		err = d.merge()
		if err != nil {
			return err
		}
		err = os.RemoveAll(partDir)
		if err != nil {
			return err
		}
		if d.onDownloadFinished != nil {
			d.onDownloadFinished(filename)
		}
	}

	return nil
}

func (d *Downloader) downloadPartial(rangeStart, rangeEnd, i int) error {

	url := d.url
	filename := d.options.FileName

	if rangeStart >= rangeEnd {
		return nil
	}

	partFilename := d.getPartFilename(filename, i)

	ctx, cancel := context.WithCancel(context.Background())
	d.mCancelFunc.Store(partFilename, cancel)
	defer d.mCancelFunc.Delete(partFilename)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	flags := os.O_CREATE | os.O_WRONLY
	if d.resume {
		flags |= os.O_APPEND
	}

	partFile, err := os.OpenFile(partFilename, flags, 0666)
	if err != nil {
		return err
	}
	defer partFile.Close()

	buf := make([]byte, 32*1024)
	_, err = io.CopyBuffer(io.MultiWriter(partFile, d.sw), resp.Body, buf)
	if err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	return nil
}

func (d *Downloader) merge() error {

	filename := d.options.FileName

	destFile, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer destFile.Close()

	for i := 0; i < d.concurrency; i++ {
		partFileName := d.getPartFilename(filename, i)
		partFile, err := os.Open(partFileName)
		if err != nil {
			return err
		}
		_, err = io.Copy(destFile, partFile)
		if err != nil {
			return err
		}
		err = partFile.Close()
		if err != nil {
			return err
		}
		err = os.Remove(partFileName)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Downloader) getPartDir(filename string) string {
	partDir := filepath.Join(d.options.BaseDir, filename)
	return partDir
}

func (d *Downloader) getPartFilename(filename string, partNum int) string {
	return fmt.Sprintf("%s/%s_%d", d.partDir, filename, partNum)
}

func (d *Downloader) singleDownload() error {

	url := d.url
	filename := d.options.FileName

	ctx, cancel := context.WithCancel(context.Background())
	d.mCancelFunc.Store(filename, cancel)
	defer d.mCancelFunc.Delete(filename)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	contentLen := int(resp.ContentLength)
	d.sw.total = contentLen

	if d.onDownloadStart != nil {
		d.onDownloadStart(contentLen, filename)
	}

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	_, err = io.CopyBuffer(io.MultiWriter(f, d.sw), resp.Body, buf)
	if err != nil {
		return err
	}

	select {
	case <-d.stopSignal:
		if d.onDownloadCanceled != nil {
			d.onDownloadCanceled(filename)
		}
		return err
	default:
		if d.onDownloadFinished != nil {
			d.onDownloadFinished(filename)
		}
	}

	return err
}
