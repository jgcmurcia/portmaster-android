package tunnel

import (
	"io"
	"sync"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
)

var (
	connections []*ConnDefaultForward
	connMutex   sync.RWMutex
)

type ConnDefaultForward struct {
	system   io.ReadWriteCloser
	remote   io.ReadWriteCloser
	endpoint tcpip.Endpoint
	idle     time.Duration
}

func addDefaultConnection(system io.ReadWriteCloser, remote io.ReadWriteCloser, endpoint tcpip.Endpoint) {
	addDefaultConnectionWithIdle(system, remote, endpoint, 0)
}

const udpIdleTimeout = 2 * time.Minute

func addDefaultConnectionWithIdle(system io.ReadWriteCloser, remote io.ReadWriteCloser, endpoint tcpip.Endpoint, idle time.Duration) {
	conn := &ConnDefaultForward{
		system: system, remote: remote, endpoint: endpoint, idle: idle,
	}

	connMutex.Lock()
	defer connMutex.Unlock()
	connections = append(connections, conn)

	conn.forward()
}

func (c *ConnDefaultForward) forward() {
	go func() {
		errc := make(chan error, 1)
		systemReader := io.Reader(c.system)
		remoteReader := io.Reader(c.remote)
		if c.idle > 0 {
			systemReader = newIdleReader(c.system, c.idle)
			remoteReader = newIdleReader(c.remote, c.idle)
		}
		go func() {
			_, err := io.Copy(c.remote, systemReader)
			errc <- err
		}()
		go func() {
			_, err := io.Copy(c.system, remoteReader)
			errc <- err
		}()
		<-errc
		onConnectionEnd(c)
	}()
}

type idleReader struct {
	reader   io.Reader
	deadline interface{ SetReadDeadline(time.Time) error }
	timeout  time.Duration
}

func newIdleReader(reader io.Reader, timeout time.Duration) io.Reader {
	deadline, ok := reader.(interface{ SetReadDeadline(time.Time) error })
	if !ok {
		return reader
	}
	return &idleReader{reader: reader, deadline: deadline, timeout: timeout}
}

func (r *idleReader) Read(p []byte) (int, error) {
	if err := r.deadline.SetReadDeadline(time.Now().Add(r.timeout)); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (c *ConnDefaultForward) close() {
	if c == nil {
		return
	}

	if c.system != nil {
		c.system.Close()
	}

	if c.endpoint != nil {
		c.endpoint.Close()
	}

	if c.remote != nil {
		c.remote.Close()
	}
}

// onConnectionEnd end connection callback
func onConnectionEnd(conn *ConnDefaultForward) {
	conn.close()

	connMutex.Lock()
	defer connMutex.Unlock()
	for i, c := range connections {
		if c == conn {
			// replace the connection with the last in the list. A faster way of removing.
			connections[i] = connections[len(connections)-1]
			connections = connections[:len(connections)-1]
			return
		}
	}
}

func endAllDefaultConnections() {
	connMutex.Lock()
	defer connMutex.Unlock()
	if connections != nil {
		for _, c := range connections {
			c.close()
		}
		connections = nil
	}
}
