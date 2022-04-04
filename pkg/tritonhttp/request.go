package tritonhttp

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

type Request struct {
	Method string // e.g. "GET"
	URL    string // e.g. "/path/to/a/file"
	Proto  string // e.g. "HTTP/1.1"

	// Header stores misc headers excluding "Host" and "Connection",
	// which are stored in special fields below.
	// Header keys are case-incensitive, and should be stored
	// in the canonical format in this map.
	Header map[string]string

	Host  string // determine from the "Host" header
	Close bool   // determine from the "Connection" header
}

// ReadRequest tries to read the next valid request from br.
//
// If it succeeds, it returns the valid request read. In this case,
// bytesReceived should be true, and err should be nil.
//
// If an error occurs during the reading, it returns the error,
// and a nil request. In this case, bytesReceived indicates whether or not
// some bytes are received before the error occurs. This is useful to determine
// the timeout with partial request received condition.
func ReadRequest(br *bufio.Reader) (req *Request, bytesReceived bool, err error) {
	header := make(map[string]string)
	req = &Request{
		Header: header,
	}

	// Read start line
	Initline, err := ReadLine(br)
	if err != nil {
		fmt.Println("Error while Reading the Init Line")
		return nil, false, err
	}
	err = req.ParseRequestInitLine(Initline)
	if err != nil {
		fmt.Println("Error while Parsing the Init Line")
		return nil, false, err
	}

	// Read headers
	for {
		line, err := ReadLine(br)
		if line == "" || err != nil {
			break
		}
		err = req.ParseRequestHeader(line)
		if err != nil {
			fmt.Println("Error: ", err)
			return nil, false, err
		}
	}

	// Check required headers
	err = req.ValidateRequest()
	if err != nil {
		fmt.Println("Error: ", err)
		return nil, false, err
	}
	return req, true, nil
}

func (r *Request) ValidateRequest() error {
	if r.Method != "GET" {
		return fmt.Errorf("Method Not Allowed  %v", r.Method)
	}
	if r.URL == "" || r.URL[0] != '/' {
		return fmt.Errorf("Got Wrong URL   %v", r.URL)
	}
	if r.Proto != "HTTP/1.1" {
		return fmt.Errorf("HTTP version not supported:  %v", r.Proto)
	}

	if r.Host == "" {
		return fmt.Errorf("Invalid Host Header:  %v", r.Proto)
	}
	return nil
}

func (r *Request) ParseRequestHeader(line string) error {
	header := regexp.MustCompile(`:\s*`).Split(line, 2)
	if len(header) == 2 {
		key := strings.ToLower(header[0])

		if (key != "" && key[(len(key)-1):] == " ") || len(key) == 0 {
			fmt.Println("Bad Key: ", line)
			return fmt.Errorf("Bad Key: %v", line)
		}
		value := header[1]
		switch key {
		case "connection":
			r.Close = false
			if strings.TrimSpace(value) == "close" {
				r.Close = true
			}
		case "host":
			r.Host = value
		default:
			r.Header[header[0]] = header[1]
		}
	} else {
		fmt.Println("Bad Header: ", line)
		return fmt.Errorf("Bad Header: %v", line)
	}
	return nil
}

func (r *Request) ParseRequestInitLine(line string) error {
	fields := strings.SplitN(line, " ", 3)
	if len(fields) != 3 {
		return fmt.Errorf("Could not parse the request line got fields: %v", fields)
	}
	r.Method = fields[0]
	r.URL = fields[1]
	r.Proto = fields[2]
	return nil
}
