# dl

[![Go Reference](https://pkg.go.dev/badge/github.com/wsshow/dl.svg)](https://pkg.go.dev/github.com/wsshow/dl)
[![Go Report Card](https://goreportcard.com/badge/github.com/wsshow/dl)](https://goreportcard.com/report/github.com/wsshow/dl)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

一个高性能的 Go 语言文件下载器库，支持多协程并发下载、断点续传和实时进度跟踪。

## ✨ 特性

- 🚀 **高性能并发下载** - 支持多协程并发下载，充分利用带宽
- 📦 **断点续传** - 下载中断后可从上次位置继续
- 📊 **实时进度** - 精确显示下载进度和速率（保留两位小数）
- 🎯 **灵活配置** - 通过函数式选项轻松配置下载行为
- 🌐 **代理支持** - 支持 HTTP、HTTPS、SOCKS5 代理和系统代理
- 🛡️ **线程安全** - 使用原子操作和互斥锁保证并发安全
- 🎮 **控制操作** - 支持开始、暂停、恢复、停止等操作
- 📝 **事件回调** - 提供下载开始、进度更新、完成和取消等回调

## 📦 安装

```shell
go get -u github.com/wsshow/dl
```

## 🚀 快速开始

### 基础用法

```go
package main

import (
	"fmt"
	"github.com/wsshow/dl"
)

func main() {
	url := "https://www.python.org/ftp/python/3.12.4/Python-3.12.4.tgz"
	
	// 创建下载器
	downloader := dl.NewDownloader(url)
	
	// 设置进度回调
	downloader.OnProgress(func(loaded, total int64, rate string) {
		progress := float64(loaded) / float64(total) * 100
		fmt.Printf("\r下载进度: %.2f%%, 速度: %s", progress, rate)
	})
	
	// 开始下载
	if err := downloader.Start(); err != nil {
		fmt.Printf("下载失败: %v\n", err)
		return
	}
	
	fmt.Println("\n下载完成!")
}
```

### 高级用法

```go
package main

import (
	"fmt"
	"github.com/wsshow/dl"
)

func main() {
	url := "https://example.com/large-file.zip"
	
	// 使用配置选项创建下载器
	downloader := dl.NewDownloader(url,
		dl.WithFileName("my-download.zip"),    // 自定义文件名
		dl.WithConcurrency(8),                  // 8个并发协程
		dl.WithBaseDir("./downloads/cache"),    // 自定义缓存目录
		dl.WithResume(true),                    // 启用断点续传
		dl.WithProxy("http://127.0.0.1:7890"), // 使用HTTP代理
	)
	
	// 设置下载开始回调
	downloader.OnDownloadStart(func(total int64, filename string) {
		fmt.Printf("开始下载: %s (大小: %d 字节)\n", filename, total)
	})
	
	// 设置进度回调
	downloader.OnProgress(func(loaded, total int64, rate string) {
		progress := float64(loaded) / float64(total) * 100
		fmt.Printf("\r进度: %.2f%% | 速度: %s | %d/%d 字节", 
			progress, rate, loaded, total)
	})
	
	// 设置完成回调
	downloader.OnDownloadFinished(func(filename string) {
		fmt.Printf("\n✅ 下载完成: %s\n", filename)
	})
	
	// 设置取消回调
	downloader.OnDownloadCanceled(func(filename string) {
		fmt.Printf("\n❌ 下载取消: %s\n", filename)
	})
	
	// 开始下载
	if err := downloader.Start(); err != nil {
		fmt.Printf("下载错误: %v\n", err)
	}
}
```

### 交互式控制

```go
package main

import (
	"fmt"
	"github.com/wsshow/dl"
)

func main() {
	url := "https://www.python.org/ftp/python/3.12.4/Python-3.12.4.tgz"
	
	downloader := dl.NewDownloader(url)
	
	downloader.OnProgress(func(loaded, total int64, rate string) {
		progress := float64(loaded) / float64(total) * 100
		fmt.Printf("\r进度: %.2f%%, 速度: %s", progress, rate)
	})
	
	downloader.OnDownloadStart(func(total int64, filename string) {
		fmt.Printf("开始下载文件: %s (大小: %d 字节)\n", filename, total)
	})
	
	downloader.OnDownloadFinished(func(filename string) {
		fmt.Printf("\n%s: 下载完成\n", filename)
	})
	
	downloader.OnDownloadCanceled(func(filename string) {
		fmt.Printf("\n%s: 下载已取消\n", filename)
	})
	
	var command string
	for {
		fmt.Println("\n命令:")
		fmt.Println("  q - 退出")
		fmt.Println("  b - 开始下载")
		fmt.Println("  s - 停止下载")
		fmt.Println("  p - 暂停下载")
		fmt.Println("  r - 恢复下载")
		fmt.Print("\n请输入命令: ")
		
		_, err := fmt.Scanln(&command)
		if err != nil {
			fmt.Println("输入错误，请重试")
			continue
		}
		
		switch command {
		case "q":
			fmt.Println("退出程序")
			return
		case "b":
			go func() {
				if err := downloader.Start(); err != nil {
					fmt.Printf("启动错误: %v\n", err)
				}
			}()
		case "s":
			if err := downloader.Stop(); err != nil {
				fmt.Printf("停止错误: %v\n", err)
			} else {
				return
			}
		case "p":
			if err := downloader.Pause(); err != nil {
				fmt.Printf("暂停错误: %v\n", err)
			}
		case "r":
			go func() {
				if err := downloader.Resume(); err != nil {
					fmt.Printf("恢复错误: %v\n", err)
				}
			}()
		default:
			fmt.Println("未知命令")
		}
	}
}
```

### 代理配置

```go
package main

import (
	"fmt"
	"github.com/wsshow/dl"
)

func main() {
	url := "https://example.com/file.zip"
	
	// 示例1: 使用 HTTP 代理
	downloader1 := dl.NewDownloader(url,
		dl.WithProxy("http://127.0.0.1:7890"),
		dl.WithFileName("file1.zip"),
	)
	
	// 示例2: 使用 SOCKS5 代理
	downloader2 := dl.NewDownloader(url,
		dl.WithProxy("socks5://127.0.0.1:1080"),
		dl.WithFileName("file2.zip"),
	)
	
	// 示例3: 使用系统代理（自动读取环境变量）
	downloader3 := dl.NewDownloader(url,
		dl.WithSystemProxy(),
		dl.WithFileName("file3.zip"),
	)
	
	// 示例4: 使用带认证的代理
	downloader4 := dl.NewDownloader(url,
		dl.WithProxy("http://username:password@proxy.example.com:8080"),
		dl.WithFileName("file4.zip"),
	)
	
	// 示例5: 自定义 HTTP 客户端（高级用法）
	customClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	downloader5 := dl.NewDownloader(url,
		dl.WithHTTPClient(customClient),
		dl.WithFileName("file5.zip"),
	)
	
	// 开始下载
	if err := downloader1.Start(); err != nil {
		fmt.Printf("下载失败: %v\n", err)
	}
}
```

**代理配置说明:**

- **HTTP/HTTPS 代理**: `WithProxy("http://proxy.com:8080")`
- **SOCKS5 代理**: `WithProxy("socks5://127.0.0.1:1080")`
- **系统代理**: `WithSystemProxy()` - 自动读取 `HTTP_PROXY`、`HTTPS_PROXY` 和 `NO_PROXY` 环境变量
- **代理认证**: 在 URL 中包含用户名和密码，如 `http://user:pass@proxy.com:8080`

## 📖 API 文档

### 创建下载器

```go
func NewDownloader(url string, opts ...OptionFunc) *Downloader
```

创建一个新的下载器实例。

**参数:**
- `url` - 要下载的文件URL地址
- `opts` - 可选的配置函数

**返回:**
- `*Downloader` - 下载器实例

### 配置选项

```go
// 设置下载文件名
func WithFileName(filename string) OptionFunc

// 设置缓存目录
func WithBaseDir(basedir string) OptionFunc

// 设置并发协程数（0表示使用CPU核心数）
func WithConcurrency(concurrency int) OptionFunc

// 设置是否启用断点续传
func WithResume(resume bool) OptionFunc

// 设置自定义HTTP客户端
func WithHTTPClient(client *http.Client) OptionFunc

// 设置代理服务器（支持 HTTP、HTTPS、SOCKS5）
func WithProxy(proxyURL string) OptionFunc

// 使用系统代理设置（读取环境变量）
func WithSystemProxy() OptionFunc
```

### 控制方法

```go
// 开始下载
func (d *Downloader) Start() error

// 停止下载
func (d *Downloader) Stop() error

// 暂停下载（Stop的别名）
func (d *Downloader) Pause() error

// 恢复下载（Start的别名）
func (d *Downloader) Resume() error
```

### 事件回调

```go
// 设置进度回调（频繁调用，避免耗时操作）
func (d *Downloader) OnProgress(f func(loaded int64, total int64, rate string))

// 设置下载开始回调
func (d *Downloader) OnDownloadStart(f func(total int64, filename string))

// 设置下载完成回调
func (d *Downloader) OnDownloadFinished(f func(filename string))

// 设置下载取消回调
func (d *Downloader) OnDownloadCanceled(f func(filename string))
```

## 🔧 配置说明

### Options 结构

```go
type Options struct {
    FileName    string       // 下载后保存的文件名（包含路径）
    BaseDir     string       // 多协程下载时分片文件的缓存目录
    Concurrency int          // 并发下载的协程数，0表示使用CPU核心数
    Resume      bool         // 是否启用断点续传功能
    HTTPClient  *http.Client // 自定义HTTP客户端，可用于配置代理、超时等
}
```

### 默认配置

- **并发数**: 等于CPU核心数
- **缓存目录**: `downloader_cache`
- **文件名**: 从URL中提取
- **断点续传**: 启用

## 🎯 性能优化

1. **并发控制**: 根据网络带宽调整并发数，通常CPU核心数是较好的起点
2. **缓冲区大小**: 默认32KB缓冲区，适合大多数场景
3. **断点续传**: 对于大文件或不稳定网络环境建议启用
4. **原子操作**: 使用`atomic`包减少锁竞争，提高并发性能

## 🔒 线程安全

该库在设计时充分考虑了并发安全：

- 使用`sync.Mutex`保护共享状态
- 使用`atomic`包进行原子操作
- 使用`sync.Map`管理取消函数
- 所有公共方法都是线程安全的

## 📝 注意事项

1. **Progress回调**: 该回调会被频繁调用，避免在其中执行耗时操作
2. **文件权限**: 确保程序对目标目录有写入权限
3. **磁盘空间**: 下载前确保有足够的磁盘空间（至少是文件大小的2倍）
4. **并发限制**: 过高的并发数可能导致服务器限流或连接失败
5. **URL有效性**: 确保提供的URL可访问且支持HTTP/HTTPS协议

## 🤝 贡献

欢迎提交Issue和Pull Request！

## 📄 许可证

本项目采用 MIT 许可证。详见 [LICENSE](LICENSE) 文件。
