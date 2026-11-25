// Built once at image build time so the standard-library packages the Go
// curriculum uses are already compiled in GOCACHE. Without this, a level that
// imports net/http spends well over the run timeout compiling it.
package main

import (
	"bufio"
	"cmp"
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"math/bits"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	_ = bufio.NewScanner
	_ = cmp.Compare[int]
	_ = heap.Init
	_ = context.Background
	_ = json.Marshal
	_ = errors.New
	_ = io.ReadAll
	_ = maps.Clone[map[int]int]
	_ = math.Sqrt
	_ = bits.OnesCount
	_ = http.NewServeMux
	_ = httptest.NewRecorder
	_ = os.Stdin
	_ = slices.Sort[[]int]
	_ = sort.Ints
	_ = strconv.Itoa
	_ = strings.ToUpper
	_ = sync.WaitGroup{}
	_ = time.Second
	_ = unicode.IsLetter
)

func main() { fmt.Println("warmup") }
