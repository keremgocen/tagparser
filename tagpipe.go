package tagpipe

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// TODO - add doc
type result struct {
	path string
	sum  [md5.Size]byte
	err  error
}

// FileCache holds info of each file, including tags observed per file
type FileCache struct {
	md5  string
	name string
	tc   TagCount
}

// Cache is used to cache parsed files, to avoid parsing the same file again
var Cache map[string]FileCache

// TagCount holds tags as key and their count
type TagCount struct {
	Key   string
	Value int
}

// SortedTagCounts used to sort TagCounts by value
type SortedTagCounts []TagCount

func (p SortedTagCounts) Len() int           { return len(p) }
func (p SortedTagCounts) Less(i, j int) bool { return p[i].Value < p[j].Value }
func (p SortedTagCounts) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// Check exits on error
func Check(e error) {
	if e != nil {
		panic(e)
	}
}

// sumFiles starts goroutines to walk the directory tree at root and digest each
// regular file.  These goroutines send the results of the digests on the result
// channel and send the result of the walk on the error channel.  If done is
// closed, sumFiles abandons its work.
func sumFiles(done <-chan struct{}, root string) (<-chan result, <-chan error) {
	// For each regular file, start a goroutine that sums the file and sends
	// the result on c.  Send the result of the walk on errc.
	c := make(chan result)
	errc := make(chan error, 1)
	go func() { // HL
		var wg sync.WaitGroup
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			wg.Add(1)
			go func() { // HL
				data, err := ioutil.ReadFile(path)
				select {
				case c <- result{path, md5.Sum(data), err}: // HL
				case <-done: // HL
				}
				wg.Done()
			}()
			// Abort the walk if done is closed.
			select {
			case <-done: // HL
				return errors.New("walk canceled")
			default:
				return nil
			}
		})
		// Walk has returned, so all calls to wg.Add are done.  Start a
		// goroutine to close c once all the sends are done.
		go func() { // HL
			wg.Wait()
			close(c) // HL
		}()
		// No select needed here, since errc is buffered.
		errc <- err // HL
	}()
	return c, errc
}

// MD5All reads all the files in the file tree rooted at root and returns a map
// from file path to the MD5 sum of the file's contents.  If the directory walk
// fails or any read operation fails, MD5All returns an error.  In that case,
// MD5All does not wait for inflight read operations to complete.
func MD5All(root string) (map[string][md5.Size]byte, error) {
	// MD5All closes the done channel when it returns; it may do so before
	// receiving all the values from c and errc.
	done := make(chan struct{}) // HLdone
	defer close(done)           // HLdone

	c, errc := sumFiles(done, root) // HLdone

	m := make(map[string][md5.Size]byte)
	for r := range c { // HLrange
		if r.err != nil {
			return nil, r.err
		}
		m[r.path] = r.sum
	}
	if err := <-errc; err != nil {
		return nil, err
	}
	return m, nil
}

// CountTagsInFile counts all given tags inside the file
func CountTagsInFile(file *strings.Reader, tag string) int {

	var telephone = regexp.MustCompile(`[A-Za-z]+`)
	// var telephone = regexp.MustCompile(`\(\d+\)\s\d+-\d+`)

	// do I need buffered channels here?
	tags := make(chan string)
	results := make(chan int)

	// I think we need a wait group, not sure.
	wg := new(sync.WaitGroup)

	// start up some workers that will block and wait?
	for w := 1; w <= 3; w++ {
		wg.Add(1)
		go MatchTags(tags, results, wg, telephone)
	}

	// Go over a file line by line and queue up a ton of work
	go func() {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			// Later I want to create a buffer of lines, not just line-by-line here ...
			tags <- scanner.Text()
		}
		close(tags)
	}()

	func() {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			// Later I want to create a buffer of lines, not just line-by-line here ...
			tags <- scanner.Text()
		}
		close(tags)
	}()

	// Now collect all the results...
	// But first, make sure we close the result channel when everything was processed
	go func() {
		wg.Wait()
		close(results)
	}()

	// Add up the results from the results channel.
	counts := 0
	for v := range results {
		counts += v
	}

	return counts
}

// MatchTags counts tags in the given file
func MatchTags(tags <-chan string, results chan<- int, wg *sync.WaitGroup, telephone *regexp.Regexp) {
	// func matchTags(tags <-chan string, results chan<- int, wg *sync.WaitGroup, telephone *regexp.Regexp) {
	// Decreasing internal counter for wait-group as soon as goroutine finishes
	defer wg.Done()

	// eventually I want to have a []string channel to work on a chunk of lines not just one line of text
	for j := range tags {
		if telephone.MatchString(j) {
			results <- 1
		}
	}
}

// IsValidJSON checks if the given string has a valid JSON format, generalized
func IsValidJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

// TimeTrack utility to measure the elapsed time in ms
func TimeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	fmt.Printf("\n%s took %s\n\n", name, elapsed)
}

// func main() {
// 	defer TimeTrack(time.Now(), "total execution")
//
//
//
// }