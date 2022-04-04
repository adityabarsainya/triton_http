package tritonhttp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var (
	Proto = "HTTP/1.1"
)

type Server struct {
	// Addr specifies the TCP address for the server to listen on,
	// in the form "host:port". It shall be passed to net.Listen()
	// during ListenAndServe().
	Addr string // e.g. ":0"

	// DocRoot specifies the path to the directory to serve static files from.
	DocRoot string
}

// ListenAndServe listens on the TCP network address s.Addr and then
// handles requests on incoming connections.
func (s *Server) ListenAndServe() error {
	if err := s.ValidateServerSetup(); err != nil {
		return errors.New("server is not setup correctly")
	}
	fmt.Println("Server setup is Valid")

	// server should now start to listen on the configured address
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}

	fmt.Println("Listening on", ln.Addr())

	// making sure the listener is closed when we exit
	defer func() {
		err = ln.Close()
		if err != nil {
			fmt.Println("error in closing listener", err)
		}
	}()

	// accept connections forever
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		fmt.Println("accepted connection", conn.RemoteAddr())
		go s.HandleConnection(conn)
	}
}

// HandleConnection reads requests from the accepted conn and handles them.
func (s *Server) HandleConnection(conn net.Conn) {

	fmt.Println("New connection accepted.", conn.RemoteAddr())

	defer fmt.Println("Connection closed.", conn.RemoteAddr())

	CRLF := "\r\n"
	requestEndDelim := CRLF + CRLF
	remaining := ""

	for {
		// Set timeout
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			fmt.Printf("Failed to set timeout for connection %v", conn.RemoteAddr())
			_ = conn.Close()
			return
		}

		buf := make([]byte, 1024*8)
		size, err := conn.Read(buf)
		if err != nil || size == 0 {
			if remaining != "" {
				fmt.Println("Timeout with partial header")
				res := Response{}
				res.HandleBadRequest()
				res.Write(conn)
				_ = conn.Close()
				return
			} else if err == io.EOF { // Handle EOF
				conn.Close()
				return
			}
		}
		if err, ok := err.(net.Error); ok && err.Timeout() {
			fmt.Printf("Connection to %v timed out", conn.RemoteAddr())
			_ = conn.Close()
			return
		}

		data := string(buf[:size])
		remaining += data
		// Read  request from the client
		if strings.Contains(remaining, requestEndDelim) {
			index := strings.LastIndex(remaining, requestEndDelim)
			requests := strings.Split(remaining[:index], requestEndDelim)
			for _, request := range requests {
				res := &Response{}
				// Handle bad request
				req, _, readErr := ReadRequest(bufio.NewReader(strings.NewReader(request + CRLF)))
				if readErr != nil {
					res.HandleBadRequest()
					res.Write(conn)
					conn.Close()
					return
				}

				// Handle good request
				res = s.HandleGoodRequest(req)
				if res.StatusCode == 404 {
					res.HandleNotFound(req)

				} else {
					res.HandleOK(req, res.FilePath)
				}
				res.Write(conn)

				// Close conn if requested
				if req.Close {
					fmt.Println("Close Connection Requested")
					conn.Close()
					return
				}
			}
			remaining = remaining[index+4:]
		}
	}
}

// HandleGoodRequest handles the valid req and generates the corresponding res.
func (s *Server) HandleGoodRequest(req *Request) (res *Response) {
	res = &Response{
		StatusCode: 200,
		Header: map[string]string{
			CanonicalHeaderKey("Date"): FormatTime(time.Now()),
		},
	}
	if req.Close {
		res.Header[CanonicalHeaderKey("Connection")] = "close"
	}
	fileName := path.Join(s.DocRoot, req.URL)
	if fileRel, err := filepath.Rel(s.DocRoot, fileName); err != nil || (len(fileRel) >= 2 && fileRel[:2] == "..") {
		fmt.Println("Escaping the root")
		res.StatusCode = 404
		return res
	} else if fileName, err := ReadFilePath(fileName, req.URL); err != nil {
		fmt.Println("File not found", fileName)
		res.StatusCode = 404
		return res
	} else {

		info, Rerr := os.Stat(fileName)
		if Rerr != nil {
			fmt.Println("File info not found", fileName, " Error", Rerr)
			res.StatusCode = 404
			return res
		}
		fileExt := filepath.Ext(fileName)
		res.Header = map[string]string{
			CanonicalHeaderKey("Date"):           FormatTime(time.Now()),
			CanonicalHeaderKey("Content-Type"):   MIMETypeByExtension(fileExt),
			CanonicalHeaderKey("Content-Length"): fmt.Sprintf("%v", info.Size()),
			CanonicalHeaderKey("Last-Modified"):  FormatTime(info.ModTime()),
		}
		if req.Close {
			res.Header[CanonicalHeaderKey("Connection")] = "close"
		}
		res.FilePath = fileName
	}
	return res
}

// HandleOK prepares res to be a 200 OK response
// ready to be written back to client.
func (res *Response) HandleOK(req *Request, path string) {
	res.StatusCode = 200
	res.Proto = Proto
}

// HandleBadRequest prepares res to be a 400 Bad Request response
// ready to be written back to client.
func (res *Response) HandleBadRequest() {
	res.StatusCode = 400
	res.Proto = Proto
	res.Header = map[string]string{
		CanonicalHeaderKey("Date"):       FormatTime(time.Now()),
		CanonicalHeaderKey("Connection"): "close",
	}
	res.FilePath = ""
}

// HandleNotFound prepares res to be a 404 Not Found response
// ready to be written back to client.
func (res *Response) HandleNotFound(req *Request) {
	res.StatusCode = 404
	res.Proto = Proto
	res.FilePath = ""
}

//Validation
func (s *Server) ValidateServerSetup() error {
	// Validating the doc root of the server
	fi, err := os.Stat(s.DocRoot)

	if os.IsNotExist(err) {
		return err
	}

	if !fi.IsDir() {
		return fmt.Errorf("doc root %q is not a directory", s.DocRoot)
	}
	return nil
}
func ReadFilePath(fileName string, url string) (string, error) {
	fileInfo, err := os.Lstat(fileName)
	if err != nil {
		fmt.Println("File Does not exists")
		return fileName, err
	}
	switch mode := fileInfo.Mode(); {
	case mode.IsRegular():
		if url[len(url)-1:] == "/" {
			return fileName, errors.New("Invalid file" + url)
		}
		return fileName, nil
	case mode.IsDir():
		if url[len(url)-1:] != "/" {
			return fileName, errors.New("Dir file" + url)
		}
		fileName = path.Join(fileName, "./index.html")
		return fileName, nil
	default:
		return fileName, nil
	}
}
