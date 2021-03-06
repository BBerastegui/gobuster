package libgobuster

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	uuid "github.com/satori/go.uuid"
)

type RedirectHandler struct {
	Transport http.RoundTripper
	State     *State
}

type RedirectError struct {
	StatusCode int
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("Redirect code: %d", e.StatusCode)
}

func (rh *RedirectHandler) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	if rh.State.FollowRedirect {
		return rh.Transport.RoundTrip(req)
	}

	resp, err = rh.Transport.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusNotModified, http.StatusUseProxy, http.StatusTemporaryRedirect:
		return nil, &RedirectError{StatusCode: resp.StatusCode}
	}

	return resp, err
}

// Make a request to the given URL.
func MakeRequest(s *State, fullUrl, cookie string) (*int, *int64, error) {
	req, err := http.NewRequest("GET", fullUrl, nil)

	if err != nil {
		return nil, nil, err
	}

	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	if s.UserAgent != "" {
		req.Header.Set("User-Agent", s.UserAgent)
	}

	if s.RandomUserAgent && s.UserAgent == "" {
		rand.Seed(time.Now().Unix())
		req.Header.Set("User-Agent", s.UserAgentsList[rand.Intn(len(s.UserAgentsList))])
	}

	if s.Username != "" {
		req.SetBasicAuth(s.Username, s.Password)
	}

	resp, err := s.Client.Do(req)

	if err != nil {
		if ue, ok := err.(*url.Error); ok {

			if strings.HasPrefix(ue.Err.Error(), "x509") {
				fmt.Println("[-] Invalid certificate")
			}

			if re, ok := ue.Err.(*RedirectError); ok {
				return &re.StatusCode, nil, ue.Err
			}
		}
		return nil, nil, err
	}

	defer resp.Body.Close()

	var length *int64 = nil

	if s.IncludeLength {
		length = new(int64)
		if resp.ContentLength <= 0 {
			body, err := ioutil.ReadAll(resp.Body)
			if err == nil {
				*length = int64(utf8.RuneCountInString(string(body)))
			}
		} else {
			*length = resp.ContentLength
		}
	}

	return &resp.StatusCode, length, nil
}

// Small helper to combine URL with URI then make a
// request to the generated location.
func GoGet(s *State, url, uri, cookie string) (*int, *int64, error) {
	return MakeRequest(s, url+uri, cookie)
}

func SetupDir(s *State) bool {
	guid := uuid.Must(uuid.NewV4())
	// TODO: Error propagation handling
	wildcardResp, _, err := GoGet(s, s.Url, fmt.Sprintf("%s", guid), s.Cookies)
	if err != nil {
		panic(err)
	}

	if s.StatusCodes.Contains(*wildcardResp) {
		s.IsWildcard = true
		fmt.Println("[-] Wildcard response found:", fmt.Sprintf("%s%s", s.Url, guid), "=>", *wildcardResp)
		if !s.WildcardForced {
			fmt.Println("[-] To force processing of Wildcard responses, specify the '-fw' switch.")
		}
		return s.WildcardForced
	}

	return true
}

func ProcessDirEntry(s *State, word string, resultChan chan<- Result) {
	suffix, prefix := "", ""
	if s.UseSlash {
		suffix = "/"
	}
	// Custom suffix will prevail if both options supplied
	if s.Suffix != "" {
		suffix = s.Suffix
	}
	if s.Prefix != "" {
		prefix = s.Prefix
	}

	// Try the DIR first
	// TODO: Error propagation handling
	dirResp, dirSize, err := GoGet(s, s.Url, prefix+word+suffix, s.Cookies)
	if err != nil {
		//panic(err)
	}
	if dirResp != nil {
		resultChan <- Result{
			Entity: prefix + word + suffix,
			Status: *dirResp,
			Size:   dirSize,
		}
	}

	// Follow up with files using each ext.
	for ext := range s.Extensions {
		file := word + s.Extensions[ext]
		// TODO: Error propagation handling
		fileResp, fileSize, err := GoGet(s, s.Url, file, s.Cookies)
		if err != nil {
			panic(err)
		}

		if fileResp != nil {
			resultChan <- Result{
				Entity: file,
				Status: *fileResp,
				Size:   fileSize,
			}
		}
	}
}

func PrintDirResult(s *State, r *Result) {
	output := ""

	// Prefix if we're in verbose mode
	if s.Verbose {
		if s.StatusCodes.Contains(r.Status) {
			output = "Found : "
		} else {
			output = "Missed: "
		}
	}

	if s.StatusCodes.Contains(r.Status) || s.Verbose {
		if s.Expanded {
			output += s.Url
		} else {
			output += "/"
		}
		output += r.Entity

		if !s.NoStatus {
			output += fmt.Sprintf(" (Status: %d)", r.Status)
		}

		if r.Size != nil {
			output += fmt.Sprintf(" [Size: %d]", *r.Size)
		}
		output += "\n"

		fmt.Printf(output)

		if s.OutputFile != nil {
			WriteToFile(output, s)
		}
	}
}
