# Install
```sh
go install github.com/sloweax/sockc@latest # binary will likely be installed at ~/go/bin
```

# Usage
```
usage: sockc [-h] [-j num] [-n val] [-o file] [-t host] [-w seconds] [file...]

options:
    -h, --help                shows usage and exits
    -j, --workers num         number of concurrent workers (default: 8)
    -n, --network val         (default: tcp)
    -o, --output file         valid proxy output file (default: stdout)
    -t, --target host         determines proxy validity by succesfully connecting
                              to host (default: google.com:443)
    -w, --timeout seconds     proxy connection timeout. 0 for no timeout (default:
                              10)
    file                      test proxies from file. if no file is provided it is
                              read from stdin
```