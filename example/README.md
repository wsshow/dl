# 通用文件下载器

一个功能强大的命令行文件下载器，支持多线程并发下载、断点续传和实时进度显示。

## 特性

- ✅ 多线程并发下载，充分利用带宽
- ✅ 实时进度条显示（保留两位小数的精确速率）
- ✅ 断点续传支持，随时中断随时继续
- ✅ 灵活的命令行参数配置
- ✅ 优雅的 Ctrl+C 中断处理
- ✅ 安静模式支持

## 安装

```bash
cd example
go build -o downloader
```

## 使用方法

### 基础用法

```bash
# 下载文件到当前目录
./downloader -url https://example.com/file.zip

# 使用简写参数
./downloader -u https://example.com/file.zip
```

### 高级用法

```bash
# 指定输出文件名
./downloader -url https://example.com/large.iso -output myfile.iso

# 设置并发数
./downloader -u https://example.com/file.zip -c 16

# 禁用断点续传
./downloader -url https://example.com/file.zip --no-resume

# 安静模式（只输出文件名）
./downloader -u https://example.com/file.zip -q

# 组合使用
./downloader -u https://example.com/archive.tar.gz -o data.tar.gz -c 8 --cache ./cache
```

### 命令行参数

| 参数           | 简写 | 说明                      | 默认值             |
| -------------- | ---- | ------------------------- | ------------------ |
| `-url`         | `-u` | 下载文件的URL地址（必需） | -                  |
| `-output`      | `-o` | 保存的文件名              | 从URL提取          |
| `-concurrency` | `-c` | 并发下载数                | CPU核心数          |
| `-cache`       | -    | 缓存目录                  | `./download_cache` |
| `-no-resume`   | -    | 禁用断点续传              | false              |
| `-quiet`       | `-q` | 安静模式                  | false              |

## 示例输出

### 正常模式

```
🚀 文件下载器
==================================
📎 URL: https://example.com/file.zip
📝 保存为: file.zip
⚡ 并发数: 8

📦 开始下载: file.zip
📊 文件大小: 524.32 MB
----------------------------------
[████████████████████░░░░░░░░░░░░░░░░░░░░] 48.52% | 254.32/524.32 MB | 12.45 MB/s    
```

### 安静模式

```bash
./downloader -u https://example.com/file.zip -q
file.zip
```

## 使用场景

### 1. 下载 Ollama 安装程序

```bash
./downloader -u https://ollama.com/download/OllamaSetup.exe -c 8
```

### 2. 下载大型 ISO 文件

```bash
./downloader -url https://releases.ubuntu.com/22.04/ubuntu-22.04-desktop-amd64.iso -c 16
```

### 3. 批量下载脚本

```bash
#!/bin/bash
urls=(
    "https://example.com/file1.zip"
    "https://example.com/file2.tar.gz"
    "https://example.com/file3.iso"
)

for url in "${urls[@]}"; do
    ./downloader -u "$url" -c 8
done
```

### 4. 在 CI/CD 中使用

```bash
# 安静模式下载，并检查退出码
./downloader -u https://example.com/package.tar.gz -q
if [ $? -eq 0 ]; then
    echo "Download successful"
else
    echo "Download failed"
    exit 1
fi
```

## 断点续传

当下载被中断（按 Ctrl+C 或网络中断）时，程序会保存已下载的分片。再次运行相同的命令即可从上次中断的位置继续下载：

```bash
# 第一次下载（中途按 Ctrl+C 中断）
./downloader -u https://example.com/large.iso

# 再次运行，自动从断点继续
./downloader -u https://example.com/large.iso
```

如果想禁用断点续传，重新下载：

```bash
./downloader -u https://example.com/large.iso --no-resume
```

## 清理缓存

下载完成后，缓存会自动清理。如需手动清理：

```bash
rm -rf download_cache
```

## 帮助信息

```bash
./downloader -h
```
