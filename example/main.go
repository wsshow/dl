package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/wsshow/dl"
)

var (
	url         string
	output      string
	concurrency int
	cacheDir    string
	noResume    bool
	quiet       bool
)

func init() {
	flag.StringVar(&url, "url", "", "下载文件的URL地址 (必需)")
	flag.StringVar(&url, "u", "", "下载文件的URL地址 (简写)")
	flag.StringVar(&output, "output", "", "保存的文件名 (默认从URL提取)")
	flag.StringVar(&output, "o", "", "保存的文件名 (简写)")
	flag.IntVar(&concurrency, "concurrency", 0, "并发下载数 (默认为CPU核心数)")
	flag.IntVar(&concurrency, "c", 0, "并发下载数 (简写)")
	flag.StringVar(&cacheDir, "cache", "./download_cache", "缓存目录")
	flag.BoolVar(&noResume, "no-resume", false, "禁用断点续传")
	flag.BoolVar(&quiet, "quiet", false, "安静模式，不显示进度条")
	flag.BoolVar(&quiet, "q", false, "安静模式 (简写)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "通用文件下载器 - 支持多线程并发下载和断点续传\n\n")
		fmt.Fprintf(os.Stderr, "用法:\n")
		fmt.Fprintf(os.Stderr, "  %s -url <URL> [选项]\n\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(os.Stderr, "选项:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n示例:\n")
		fmt.Fprintf(os.Stderr, "  %s -url https://example.com/file.zip\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(os.Stderr, "  %s -u https://example.com/file.zip -o myfile.zip -c 8\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(os.Stderr, "  %s -url https://example.com/large.iso --no-resume\n", filepath.Base(os.Args[0]))
	}
}

func main() {
	flag.Parse()

	// 验证必需参数
	if url == "" {
		fmt.Fprintf(os.Stderr, "❌ 错误: 必须指定下载URL\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// 如果未指定输出文件名，从URL中提取
	if output == "" {
		output = filepath.Base(url)
		if output == "/" || output == "." {
			output = "downloaded_file"
		}
	}

	if !quiet {
		fmt.Println("🚀 文件下载器")
		fmt.Println("==================================")
		fmt.Printf("📎 URL: %s\n", url)
		fmt.Printf("📝 保存为: %s\n", output)
		if concurrency > 0 {
			fmt.Printf("⚡ 并发数: %d\n", concurrency)
		}
	}

	// 创建下载器配置
	opts := []dl.OptionFunc{
		dl.WithFileName(output),
		dl.WithBaseDir(cacheDir),
		dl.WithResume(!noResume),
	}
	if concurrency > 0 {
		opts = append(opts, dl.WithConcurrency(concurrency))
	}

	downloader := dl.NewDownloader(url, opts...)

	// 设置下载开始回调
	downloader.OnDownloadStart(func(total int64, filename string) {
		if !quiet {
			fmt.Printf("\n📦 开始下载: %s\n", filename)
			fmt.Printf("📊 文件大小: %.2f MB\n", float64(total)/(1024*1024))
			fmt.Println("----------------------------------")
		}
	})

	// 设置进度回调
	if !quiet {
		var lastProgress float64
		downloader.OnProgress(func(loaded, total int64, rate string) {
			progress := float64(loaded) / float64(total) * 100

			// 只在进度变化超过0.5%时更新显示
			if progress-lastProgress >= 0.5 || progress >= 100 {
				lastProgress = progress

				// 计算已下载和总大小（MB）
				loadedMB := float64(loaded) / (1024 * 1024)
				totalMB := float64(total) / (1024 * 1024)

				// 生成进度条
				barWidth := 40
				filledWidth := int(progress / 100 * float64(barWidth))
				bar := ""
				for i := 0; i < barWidth; i++ {
					if i < filledWidth {
						bar += "█"
					} else {
						bar += "░"
					}
				}

				// 显示进度
				fmt.Printf("\r[%s] %.2f%% | %.2f/%.2f MB | %s    ",
					bar, progress, loadedMB, totalMB, rate)
			}
		})
	}

	// 设置下载完成回调
	downloader.OnDownloadFinished(func(filename string) {
		if quiet {
			fmt.Printf("%s\n", filename)
		} else {
			fmt.Printf("\n\n✅ 下载完成: %s\n", filename)
			fmt.Println("==================================")
		}
	})

	// 设置下载取消回调
	downloader.OnDownloadCanceled(func(filename string) {
		if !quiet {
			fmt.Printf("\n\n⚠️  下载已取消: %s\n", filename)
			if !noResume {
				fmt.Println("提示: 由于启用了断点续传，可以重新运行相同命令继续下载。")
			}
			fmt.Println("==================================")
		}
	})

	// 设置信号处理，支持 Ctrl+C 优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 启动下载
	errChan := make(chan error, 1)
	go func() {
		errChan <- downloader.Start()
	}()

	// 等待下载完成或用户中断
	select {
	case err := <-errChan:
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n❌ 下载错误: %v\n", err)
			os.Exit(1)
		}
	case <-sigChan:
		if !quiet {
			fmt.Println("\n\n⏸️  接收到中断信号，正在停止下载...")
		}
		if err := downloader.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "停止下载时出错: %v\n", err)
		}
		// 等待下载协程完全停止
		<-errChan
		os.Exit(130) // 128 + SIGINT(2)
	}
}
