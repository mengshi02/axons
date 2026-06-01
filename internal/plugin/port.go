package plugin

import (
	"fmt"
	"net"
	"sync"
)

// PortAllocator manages port allocation for plugin backend processes.
// It ensures no port conflicts and holds listeners to prevent TOCTOU races.
type PortAllocator struct {
	mu        sync.Mutex
	used      map[int]string      // port → pluginId
	listeners map[int]net.Listener // port → listener (held open to prevent port stealing)
}

// NewPortAllocator creates a new PortAllocator.
func NewPortAllocator() *PortAllocator {
	return &PortAllocator{
		used:      make(map[int]string),
		listeners: make(map[int]net.Listener),
	}
}

// Allocate allocates a dynamic port for a plugin.
// The returned listener is held open (not closed) to prevent the port from
// being reassigned by the OS before the plugin process binds to it.
// The port number is injected via stdin so the plugin can bind without a race.
func (pa *PortAllocator) Allocate(pluginID string) (int, net.Listener, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, fmt.Errorf("port allocate: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	pa.used[port] = pluginID
	pa.listeners[port] = listener

	return port, listener, nil
}

// AllocateFixed attempts to allocate a specific fixed port for a plugin.
// Returns an error if the port is already allocated to another plugin.
func (pa *PortAllocator) AllocateFixed(pluginID string, port int) (int, net.Listener, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	if existingPlugin, ok := pa.used[port]; ok {
		if existingPlugin != pluginID {
			return 0, nil, fmt.Errorf("port %d already allocated to plugin %s", port, existingPlugin)
		}
		// Same plugin already has this port — return existing
		return port, pa.listeners[port], nil
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return 0, nil, fmt.Errorf("port allocate fixed %d: %w", port, err)
	}

	pa.used[port] = pluginID
	pa.listeners[port] = listener

	return port, listener, nil
}

// Release releases the port allocated to a plugin.
// This closes the held listener, making the port available to the OS.
func (pa *PortAllocator) Release(pluginID string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	for port, id := range pa.used {
		if id == pluginID {
			if ln, ok := pa.listeners[port]; ok {
				ln.Close()
				delete(pa.listeners, port)
			}
			delete(pa.used, port)
			return
		}
	}
}

// GetPort returns the port allocated to a plugin, if any.
func (pa *PortAllocator) GetPort(pluginID string) (int, bool) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	for port, id := range pa.used {
		if id == pluginID {
			return port, true
		}
	}
	return 0, false
}

// IsAllocated checks if a port is already allocated.
func (pa *PortAllocator) IsAllocated(port int) bool {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	_, ok := pa.used[port]
	return ok
}