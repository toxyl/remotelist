# RemoteList

RemoteList is a Go package that provides functionalities for working with a remote list such as IP blocklists.  

It downloads a file from a remote location, runs a filter function over the content and stores the results to disk. If the local file already exists and is not older than a specificed interval then downloading and filtering is skipped. The data stored on disk is then loaded into memory where each line represents one record. During reading a transform function is executed on each record before storing it in memory. The resulting RemoteList struct provides functions to search the data and temporarily add records to it.

## Installation

To install RemoteList, use `go get`:

```bash
go get github.com/toxyl/remotelist
```

## Usage
### Default behavior

The default `Filter` function removes all empty lines as well as those that start with `#` or `//`.  
The default `Search` and `Has` functions operate case-insensitive.  

#### Example

```go
package main

import (
	"fmt"
	"github.com/toxyl/remotelist"
)

func main() {
	// Create a new RemoteList instance
	rl, err := remotelist.NewSimple("list.txt", "http://www.example.com/list.txt", 24 * time.Hour)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Use RemoteList methods
	exists := rl.Has("value")
	results := rl.Search("term")
	fmt.Println("Exists:", exists)
	fmt.Println("Search Results:", results)

	// Add a new value to the list
	rl.Add("new_value")
}
```

### Custom behavior

You can also customize the behavior of RemoteList by providing custom functions during initialization. 

#### Example: oSSH hosts list
The list only contains IPs, but the default `Has` function will lowercase everything, therefore adding unnecessary operations. Instead we implement our own `Has` function that avoids the lowercasing step.
```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/toxyl/remotelist"
)

func main() {
	listOSSH, err := remotelist.New(
		filepath.Join(os.TempDir(), "oSSHHosts.txt"),
		"https://raw.githubusercontent.com/toxyl/ossh-wordlists/master/hosts.txt",
		24*time.Hour,
		func(records map[string]struct{}, term string) bool {
			_, ok := records[term]
			return ok
		},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	
	if err != nil {
		panic(err)
	}
	
	if listOSSH.Has("1.2.3.4") {
		fmt.Println("IP found")
	} else {
		fmt.Println("IP _not_ found")
	}
}
```

