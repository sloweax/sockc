# Install
```sh
go install github.com/sloweax/sockc@latest # binary will likely be installed at ~/go/bin
```

# Usage
```
usage: sockc [-h] [-a] [-d] [-j num] [-n val] [-o file] [-t host] [-u] [-w seconds]
             [-x url] [file...]

options:
    -h, --help                shows usage and exits
    -a, --append              open output file in append mode
    -d, --detail              include the time to establish connection in the fragment
                              of the proxy url (in seconds)
    -j, --workers num         number of concurrent workers (default: 8)
    -n, --network val         (default: tcp)
    -o, --output file         valid proxy output file (default: stdout)
    -t, --target host         determines proxy validity by succesfully connecting
                              to host (default: google.com:443)
    -u, --unique              don't scan the same proxy url twice
    -w, --timeout seconds     proxy connection timeout. 0 for no timeout (default:
                              10)
    -x, --proxy url           use proxy to connect to proxy
    file                      test proxies from file. if no file is provided it is
                              read from stdin
```

# Example
```sh
$ cat proxies.txt
socks5://123.123.123.123:123
socks5://321.321.321.321:321

$ cat proxies.txt | sockc
socks5://123.123.123.123:123
# errors are printed to stderr
socks connect tcp 321.321.321.321:321->google.com:443: dial tcp: lookup 321.321.321.321: no such host
```

# Supported protocols

- socks5

custom protocols are supported by implementing `golang.org/x/net/proxy`'s `proxy.ContextDialer` and registering it with `proxy.RegisterDialerType()`
