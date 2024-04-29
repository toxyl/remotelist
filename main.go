package remotelist

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// A `SearchFunc` is used to search the given `records` and return a list of all matches.
type SearchFunc func(records map[string]struct{}, term string) (matches []string)

// A `HasFunc` is used to determine if a `term` exists in the given `records`. If so, it returns `true`.
type HasFunc func(records map[string]struct{}, term string) (exists bool)

// A `DataFilterFunc` is run over the downloaded content before it is saved to disk.
//
// This function can be used to filter out content before the file is saved to disk.
type DataFilterFunc func(source string) string

// A `DataLineFunc` is run over each line of the locally stored file when reading it.
//
// This function can be used to transform lines on the fly as well as exclude them from the index (`include = false`).
type DataLineFunc func(line string) (parsed string, include bool)

var (
	// The default `Search` function performs a case-insensitive search for all records that contain
	// the search term and returns a slice with the results.
	DefaultSearchFunc = func(records map[string]struct{}, term string) []string {
		term = strings.ToLower(term)
		res := []string{}
		for rec := range records {
			if strings.Contains(strings.ToLower(rec), term) {
				res = append(res, rec)
			}
		}
		sort.Strings(res)
		return res
	}

	// The default `DataLineProcess` function is executed on each line of the locally downloaded file.
	// It returns the input line and sets `include` to `false` if the line is empty or starts with `#` or `//`.
	// That indicates to the parser that the line should be dropped.
	DefaultDataLineProcessFunc = func(line string) (parsed string, include bool) {
		return line, !(strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || len(line) == 0)
	}

	// The default `Has` function checks `records` has the search `term`. Matching is case-insensitive.
	DefaultHasFunc = func(records map[string]struct{}, term string) bool {
		term = strings.ToLower(term)
		for rec := range records {
			if strings.EqualFold(rec, term) {
				return true
			}
		}
		return false
	}

	// The default `HasPrefix` function checks if any record starts with the search term. Matching is case-insensitive.
	DefaultHasPrefixFunc = func(records map[string]struct{}, term string) bool {
		term = strings.ToLower(term)
		for rec := range records {
			if strings.HasPrefix(strings.ToLower(rec), term) {
				return true
			}
		}
		return false
	}

	// The default `HasSuffix` function checks if any record ends with the search term. Matching is case-insensitive.
	DefaultHasSuffixFunc = func(records map[string]struct{}, term string) bool {
		term = strings.ToLower(term)
		for rec := range records {
			if strings.HasSuffix(strings.ToLower(rec), term) {
				return true
			}
		}
		return false
	}
)

// RemoteList represents a remote list and provides methods for managing it.
type RemoteList struct {
	fnSearch    SearchFunc     // Function for searching a term in the list
	fnHas       HasFunc        // Function for checking if a term exists in the list
	fnHasPrefix HasFunc        // Function for checking if a prefix exists in the list
	fnHasSuffix HasFunc        // Function for checking if a suffix exists in the list
	fnDataFiler DataFilterFunc // Function for preprocessing data before writing to file
	fnDataLine  DataLineFunc   // Function for processing each line of data read from file
	maxAge      time.Duration  // Maximum age of the local list file before redownloading
	fileLocal   string         // Filepath for storing the list locally
	fileRemote  string         // Filepath from which to download the list
	mu          *sync.Mutex
	records     map[string]struct{} // records stores the data from the list file
}

// Has checks if a value exists in the RemoteList
func (rl *RemoteList) Has(value string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.fnHas(rl.records, value)
}

// Search searches for a value in the RemoteList and returns matching results
func (rl *RemoteList) Search(value string) []string {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.fnSearch(rl.records, value)
}

// Add adds a value to the RemoteList
func (rl *RemoteList) Add(value string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	value = strings.TrimSpace(value)
	rl.records[value] = struct{}{}
}

// List returns the data stored in the RemoteList as a sorted string slice
func (rl *RemoteList) List() []string {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	res := []string{}
	for rec := range rl.records {
		res = append(res, rec)
	}
	sort.Strings(res)
	return res
}

// download downloads the list from the remote location if necessary
func (rl *RemoteList) download() error {
	// Check if download is needed based on file's last modification time
	needsDownload := true
	fileInfo, err := os.Stat(rl.fileLocal)
	fileExists := err == nil
	if fileExists && time.Since(fileInfo.ModTime()) < rl.maxAge {
		needsDownload = false
	}

	// Perform download if necessary
	if needsDownload {
		resp, err := http.Get(rl.fileRemote)
		if err != nil {
			return fmt.Errorf("list download failed: %s", err.Error())
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("list download failed with status code: %d", resp.StatusCode)
		}

		// Read response body and write to local file
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("list download failed, could not read response: %s", err.Error())
		}

		// Optionally preprocess data before writing to file
		permissions := os.FileMode(0644)
		if fileExists {
			permissions = fileInfo.Mode().Perm()
		}

		if rl.fnDataFiler == nil {
			err = os.WriteFile(rl.fileLocal, data, permissions)
		} else {
			err = os.WriteFile(rl.fileLocal, []byte(rl.fnDataFiler(string(data))), permissions)
		}

		if err != nil {
			return fmt.Errorf("list download failed, could not write data: %s", err.Error())
		}
	}
	return nil
}

// init initializes the RemoteList by reading data from the local file
func (rl *RemoteList) init() error {
	// Read file data
	fileData, err := os.ReadFile(rl.fileLocal)
	if err != nil {
		return fmt.Errorf("error reading local file: %s", err)
	}

	// Process each line of data and populate records map
	for _, line := range strings.Split(string(fileData), "\n") {
		if rl.fnDataLine != nil {
			if str, ok := rl.fnDataLine(line); ok {
				rl.Add(str)
			}
		}
	}

	return nil
}

// New creates a new RemoteList instance with the specified parameters
func New(
	fileLocal, fileRemote string,
	maxAge time.Duration,
	fnHas, fnHasPrefix, fnHasSuffix HasFunc,
	fnSearch SearchFunc,
	fnDataFilter DataFilterFunc,
	fnDataLine DataLineFunc,
) (*RemoteList, error) {
	// Initialize RemoteList struct
	rl := &RemoteList{
		mu:          &sync.Mutex{},
		maxAge:      maxAge,
		fileLocal:   fileLocal,
		fileRemote:  fileRemote,
		fnSearch:    fnSearch,
		fnHas:       fnHas,
		fnHasPrefix: fnHasPrefix,
		fnHasSuffix: fnHasSuffix,
		fnDataFiler: fnDataFilter,
		fnDataLine:  fnDataLine,
		records:     map[string]struct{}{},
	}

	// Set default functions if not provided
	if fnHas == nil {
		rl.fnHas = DefaultHasFunc
	}
	if fnHasPrefix == nil {
		rl.fnHasPrefix = DefaultHasPrefixFunc
	}
	if fnHasSuffix == nil {
		rl.fnHasSuffix = DefaultHasSuffixFunc
	}

	if fnSearch == nil {
		rl.fnSearch = DefaultSearchFunc
	}

	if fnDataLine == nil {
		rl.fnDataLine = DefaultDataLineProcessFunc
	}

	// Download and initialize the list
	if err := rl.download(); err != nil {
		return nil, err
	}

	return rl, rl.init()
}

func NewSimple(fileLocal, fileRemote string, maxAge time.Duration) (*RemoteList, error) {
	return New(fileLocal, fileRemote, maxAge, nil, nil, nil, nil, nil, nil)
}
