package main

import (
	"bufio"
	"context"
	"crypto/tls"
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
	_ "github.com/sloweax/sockc/shadowsocks"
	"golang.org/x/net/proxy"
)

type Program struct {
	Append     bool     `name:"a" alias:"append" description:"open output file in append mode"`
	Detail     bool     `name:"d" alias:"detail" description:"include the time to establish connection in the fragment of the proxy url (in seconds)"`
	Workers    uint     `name:"j" alias:"workers" metavar:"num" description:"number of concurrent workers (default: 8)"`
	Network    string   `name:"n" alias:"network" metavar:"val" description:"network to connect to -t (default: tcp)"`
	OutputFile string   `name:"o" alias:"output" metavar:"file" description:"valid proxy output file (default: stdout)"`
	Target     string   `name:"t" alias:"target" metavar:"host" description:"target host to check proxy validity. see \"-v\" option (default: google.com:443)"`
	Unique     bool     `name:"u" alias:"unique" description:"don't scan the same proxy url twice"`
	Validity   string   `name:"v" alias:"validity" description:"connect: validity by just connecting, tls: validity by succesfull tls handshake (default: tls)"`
	Timeout    uint     `name:"w" alias:"timeout" metavar:"seconds" description:"proxy connection timeout. 0 for no timeout (default: 10)"`
	Proxy      *string  `name:"x" alias:"proxy" metavar:"url" description:"use proxy to connect to proxies"`
	ProxyFiles []string `name:"file" type:"positional" metavar:"file..." description:"check proxies from file. if no file is provided it is read from stdin"`

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

	switch p.Validity {
	case "connect", "tls":
	default:
		return fmt.Errorf(`unsupported value for option -v %q`, p.Validity)
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
		flags := os.O_WRONLY | os.O_CREATE
		if p.Append {
			flags |= os.O_APPEND
		}
		if f, err := os.OpenFile(p.OutputFile, flags, 0644); err != nil {
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

func (p *Program) CheckProxy(url *url.URL) error {
	var pd proxy.Dialer

	if p.Proxy != nil {
		tmp, err := proxy.FromURL(p.proxyUrl, nil)
		if err != nil {
			return err
		}
		pd = tmp
	}

	d, err := proxy.FromURL(url, pd)
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
	defer conn.Close()

	switch p.Validity {
	case "tls":
		host, _, err := net.SplitHostPort(p.Target)
		if err != nil {
			return err
		}
		tlsconn := tls.Client(conn, &tls.Config{ServerName: host})
		conn = tlsconn
		if err := tlsconn.HandshakeContext(ctx); err != nil {
			return err
		}
	case "connect":
	}

	return nil
}

func (p *Program) Worker() {
	for proxy := range p.pchan {
		start := time.Now()
		err := p.CheckProxy(proxy)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}

		if p.Detail {
			proxy.Fragment = strconv.FormatFloat(time.Now().Sub(start).Seconds(), 'f', -1, 64)
		}

		fmt.Fprintln(p.fout, proxy.String())
	}
}

func main() {
	p := &Program{
		Network:    "tcp",
		Target:     "google.com:443",
		OutputFile: "-",
		Validity:   "tls",
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
