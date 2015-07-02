package apns

import (
	"crypto/tls"
	"log"
	"net"
	"strings"
	"time"
)

type sentNotif struct {
	pn       *PushNotification
	sentTime time.Time
}

func (c *Client) Loop() error {
	c.sendChan = make(chan *PushNotification, 10)
	if err := c.connect(); err != nil {
		return err
	}
	go c.loop()
	return nil
}
func (c *Client) loop() {
	defer c.pConn.Close()

	closeChan := make(chan bool)
	resetChan := make(chan bool)

	go c.sendLoop(closeChan, resetChan)
	c.recvLoop(closeChan, resetChan)

	log.Println("loop ended")
}

func (c *Client) SendAsync(pn *PushNotification) {
	c.sendChan <- pn
}
func (c *Client) recvLoop(closeChan, resetChan chan bool) {
	// This channel will contain the binary response
	// from Apple in the event of a failure.
	responseChannel := make(chan []byte, 1)
	readFunc := func() {
		for {
			buffer := make([]byte, 6, 6)
			_, err := c.pConn.Read(buffer)
			if err != nil {
				if nerr, ok := err.(net.Error); ok && !nerr.Temporary() {
					<-resetChan
				}
				continue
			}
			responseChannel <- buffer
		}
	}
	go readFunc()
	for {
		select {
		case r := <-responseChannel:
			log.Printf("recvLoop() response:%v\n", r)
		case <-closeChan:
			log.Printf("recvLoop() closeChan")
			break
		}
	}
	log.Println("recvLoop() exited")
}
func (c *Client) sendLoop(closeChan, resetChan chan bool) {
	log.Println("starting send loop")
	for {
		log.Println("waiting message to send")
		select {
		case pn := <-c.sendChan:
			// convert to bytes
			payload, err := pn.ToBytes()
			if err != nil {
				continue
			}

			for i := 0; i < 3; i++ {
				_, err = c.pConn.Write(payload)
				if err != nil {
					log.Printf("sendLoop() error writing:%v, sleep for now", err)
					time.Sleep(5 * time.Second)

					if nerr, ok := err.(net.Error); ok && !nerr.Temporary() {
						log.Printf("sendLoop() permanent errors:%v", err)
						if err := c.reconnect(); err != nil {
							break
						}
						resetChan <- true
					}
				}

			}
		case <-closeChan:
			break
		}

	}
	log.Println("send loop ended")
}

func (client *Client) reconnect() error {
	client.pConn.Close()
	return client.connect()
}

// connect to APN server
func (client *Client) connect() error {
	var cert tls.Certificate
	var err error

	if len(client.CertificateBase64) == 0 && len(client.KeyBase64) == 0 {
		// The user did not specify raw block contents, so check the filesystem.
		cert, err = tls.LoadX509KeyPair(client.CertificateFile, client.KeyFile)
	} else {
		// The user provided the raw block contents, so use that.
		cert, err = tls.X509KeyPair([]byte(client.CertificateBase64), []byte(client.KeyBase64))
	}

	if err != nil {
		return err
	}

	gatewayParts := strings.Split(client.Gateway, ":")
	conf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ServerName:   gatewayParts[0],
	}

	conn, err := net.Dial("tcp", client.Gateway)
	if err != nil {
		return err
	}

	client.pConn = tls.Client(conn, conf)
	return client.pConn.Handshake()
}
