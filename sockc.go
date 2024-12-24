package main

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sloweax/argparse"
	"golang.org/x/net/proxy"
)

type Program struct {
	Workers    uint     `name:"j" alias:"workers" metavar:"num" description:"number of concurrent workers (default: 8)"`
	Network    string   `name:"n" alias:"network" metavar:"val" description:"(default: tcp)"`
	OutputFile string   `name:"o" alias:"output" metavar:"file" description:"valid proxy output file (default: stdout)"`
	Target     string   `name:"t" alias:"target" metavar:"host" description:"determines proxy validity by succesfully connecting to host (default: google.com:443)"`
	Timeout    uint     `name:"w" alias:"timeout" metavar:"seconds" description:"proxy connection timeout. 0 for no timeout (default: 10)"`
	ProxyFiles []string `name:"file" type:"positional" metavar:"file..." description:"test proxies from file. if no file is provided it is read from stdin"`

	fout  *os.File
	pchan chan *url.URL
}

func (p *Program) Init() error {
	p.pchan = make(chan *url.URL)

	if p.OutputFile == "-" {
		p.fout = os.Stdout
	} else {
		if f, err := os.OpenFile(p.OutputFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644); err != nil {
			return err
		} else {
			p.fout = f
		}
	}

	return nil
}

func (p *Program) LoadFile(f *os.File) error {
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		proxy := strings.TrimFunc(scanner.Text(), unicode.IsSpace)
		if len(proxy) == 0 {
			continue
		}
		url, err := url.Parse(proxy)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid url %s\n", proxy)
			continue
		}
		p.pchan <- url
	}

	return scanner.Err()
}

func (p *Program) Run() error {
	if err := p.Init(); err != nil {
		return err
	}

	wg := &sync.WaitGroup{}

	for i := uint(0); i < p.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Worker()
		}()
	}

	if len(p.ProxyFiles) == 0 {
		p.ProxyFiles = append(p.ProxyFiles, "-")
	}

	for _, fname := range p.ProxyFiles {
		var err error
		var f *os.File
		if fname == "-" {
			f = os.Stdin
		} else {
			f, err = os.Open(fname)
			if err != nil {
				return err
			}
		}

		err = p.LoadFile(f)
		f.Close()
		if err != nil {
			return err
		}
	}

	close(p.pchan)
	wg.Wait()

	return p.fout.Close()
}

func (p *Program) CheckProxy(url *url.URL) error {
	d, err := proxy.FromURL(url, nil)
	if err != nil {
		return err
	}

	dc, ok := d.(proxy.ContextDialer)
	if !ok {
		return fmt.Errorf("%s does not implement proxy.ContextDialer", url.Scheme)
	}

	var ctx context.Context
	var cancel context.CancelFunc

	if p.Timeout == 0 {
		ctx = context.Background()
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), time.Second*time.Duration(p.Timeout))
		defer cancel()
	}

	conn, err := dc.DialContext(ctx, p.Network, p.Target)
	if err != nil {
		return err
	}
	conn.Close()

	return nil
}

func (p *Program) Worker() {
	for proxy := range p.pchan {
		err := p.CheckProxy(proxy)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		fmt.Fprintln(p.fout, proxy.String())
	}
}

func main() {
	p := &Program{
		Network:    "tcp",
		Target:     "google.com:443",
		OutputFile: "-",
		Workers:    8,
		Timeout:    10,
	}

	parser := argparse.FromStruct(p)
	if err := parser.ParseArgs(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
