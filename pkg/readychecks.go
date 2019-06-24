package pkg

import (
	"log"
	"net"
	"strconv"
	"time"
)

//DNSResolves checks if the domain resultion works
func DNSResolves(apiDNS string) {
	for {
		ips, err := net.LookupIP(apiDNS)
		if err == nil {
			for _, ip := range ips {
				log.Println("%s IN A %s", apiDNS, ip.String())
			}
			return
		}
	}
}

//KubeUp checks if kubernetes is running
func KubeUp(apiDNS string, apiPort int) bool {
	retry := 0
	for {
		_, err := net.Dial("tcp", apiDNS+":"+strconv.Itoa(apiPort))
		if err == nil {
			return true
		}
		if retry > 2 {
			return false
		}
		retry++
		time.Sleep(time.Millisecond * 100)
	}
}
