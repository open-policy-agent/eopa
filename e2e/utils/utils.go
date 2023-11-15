package utils

import (
	"fmt"
	"net"
	"time"
)

// Tries an open port 3x times with short delays between each time to ensure the port is really free.
func IsTCPPortBindable(port int) bool {
	portOpen := true
	for i := 0; i < 3; i++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln.Close()
		}
		portOpen = portOpen && err == nil
		time.Sleep(time.Millisecond) // Adjust the delay as needed
	}
	return portOpen
}
