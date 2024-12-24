package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sloweax/argparse"
	"golang.org/x/net/proxy"
)

type DialerWithConn interface {
	DialWithConn(context.Context, net.Conn, string, string) (net.Addr, error)
}

type Program struct {
	Detail     bool     `name:"d" alias:"detail" description:"include the time to establish connection in the fragment of the proxy url (in seconds, excluding -x connection time)"`
	Workers    uint     `name:"j" alias:"workers" metavar:"num" description:"number of concurrent workers (default: 8)"`
	Network    string   `name:"n" alias:"network" metavar:"val" description:"(default: tcp)"`
	OutputFile string   `name:"o" alias:"output" metavar:"file" description:"valid proxy output file (default: stdout)"`
	Target     string   `name:"t" alias:"target" metavar:"host" description:"determines proxy validity by succesfully connecting to host (default: google.com:443)"`
	Unique     bool     `name:"u" alias:"unique" description:"don't scan the same proxy url twice"`
	Timeout    uint     `name:"w" alias:"timeout" metavar:"seconds" description:"proxy connection timeout. 0 for no timeout. it does not count -x connection time (default: 10)"`
	Proxy      *string  `name:"x" alias:"proxy" metavar:"url" description:"use proxy to connect to proxy"`
	ProxyFiles []string `name:"file" type:"positional" metavar:"file..." description:"test proxies from file. if no file is provided it is read from stdin"`

	proxyUrl  *url.URL
	scannedmu *sync.Mutex
	scanned   map[string]struct{}
	fout      *os.File
	pchan     chan *url.URL
}

func (p *Program) Init() error {
	if p.Unique {
		p.scanned = make(map[string]struct{})
		p.scannedmu = new(sync.Mutex)
	}

	p.pchan = make(chan *url.URL)

	if p.Proxy != nil {
		url, err := url.Parse(*p.Proxy)
		if err != nil {
			return err
		}
		p.proxyUrl = url
	}

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
		if p.Unique {
			key := url.String()
			p.scannedmu.Lock()
			_, ok := p.scanned[key]
			if !ok {
				p.scanned[key] = struct{}{}
				p.scannedmu.Unlock()
			} else {
				p.scannedmu.Unlock()
				continue
			}
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

func (p *Program) CheckProxy(url *url.URL) (time.Duration, error) {
	d, err := proxy.FromURL(url, nil)
	if err != nil {
		return 0, err
	}

	if p.Proxy != nil {
		pd, ok := d.(DialerWithConn)
		if !ok {
			return 0, fmt.Errorf("%s does not implement DialerWithConn", url.Scheme)
		}

		ppd, err := proxy.FromURL(p.proxyUrl, nil)
		if err != nil {
			return 0, err
		}

		pc, err := ppd.Dial(p.Network, url.Host)
		if err != nil {
			return 0, err
		}
		defer pc.Close()

		var ctx context.Context
		var cancel context.CancelFunc

		if p.Timeout == 0 {
			ctx = context.Background()
		} else {
			ctx, cancel = context.WithTimeout(context.Background(), time.Second*time.Duration(p.Timeout))
			defer cancel()
		}

		start := time.Now()
		_, err = pd.DialWithConn(ctx, pc, p.Network, p.Target)

		return time.Now().Sub(start), err
	} else {
		dc, ok := d.(proxy.ContextDialer)
		if !ok {
			return 0, fmt.Errorf("%s does not implement proxy.ContextDialer", url.Scheme)
		}

		var ctx context.Context
		var cancel context.CancelFunc

		if p.Timeout == 0 {
			ctx = context.Background()
		} else {
			ctx, cancel = context.WithTimeout(context.Background(), time.Second*time.Duration(p.Timeout))
			defer cancel()
		}

		start := time.Now()
		conn, err := dc.DialContext(ctx, p.Network, p.Target)
		if err != nil {
			return 0, err
		}
		defer conn.Close()

		return time.Now().Sub(start), nil
	}
}

func (p *Program) Worker() {
	for proxy := range p.pchan {
		dur, err := p.CheckProxy(proxy)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}

		if p.Detail {
			proxy.Fragment = strconv.FormatFloat(dur.Seconds(), 'f', -1, 64)
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
