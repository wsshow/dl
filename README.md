# dl

## Installation

```shell
go get -u github.com/wsshow/dl
```

## Example

```go
package main

import (
	"fmt"

	"github.com/wsshow/dl"
)

func main() {

	url := "https://www.python.org/ftp/python/3.12.4/Python-3.12.4.tgz"

	ndl := dl.NewDownloader(url)
	ndl.OnProgress(func(cur, total int, rate string) {
		fmt.Printf("\rprogress: %.2f%%, rate: %s", float64(cur)/float64(total)*100, rate)
	})
	ndl.OnDownloadStart(func(total int, filename string) {
		fmt.Printf("Start downloading files: %s\n", filename)
	})
	ndl.OnDownloadFinished(func(filename string) {
		fmt.Printf("\n%s: Download completed\n", filename)
	})
	ndl.OnDownloadCanceled(func(filename string) {
		fmt.Printf("\n%s: Download cancellation\n", filename)
	})

	var s string
	for {
		fmt.Println(`help:
	q to quit,
	b to start,
	s to stop,
	p to pause,
	r to resume`)
		fmt.Printf("Please enter instructions:")
		_, err := fmt.Scanln(&s)
		if err != nil {
			fmt.Println("Please enter q to quit, b to start, s to stop, p to pause, r to resume")
			continue
		}
		switch s {
		case "q":
			return
		case "b":
			go func() {
				err := ndl.Start()
				if err != nil {
					fmt.Println("Start:", err)
					return
				}
			}()
		case "s":
			err = ndl.Stop()
			if err != nil {
				fmt.Println("Stop:", err)
				continue
			}
			return
		case "p":
			err = ndl.Pause()
			if err != nil {
				fmt.Println("Pause:", err)
				continue
			}
		case "r":
			go func() {
				err := ndl.Resume()
				if err != nil {
					fmt.Println("Resume:", err)
					return
				}
			}()
		default:
			continue
		}
	}

}
```
