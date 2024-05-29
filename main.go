package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	// libp2p
	dht "github.com/libp2p/go-libp2p-kad-dht"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	libp2p "github.com/libp2p/go-libp2p"
	libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
)

const (
	RendezvousString = "test18471398571"
	contextTimeout = 60 * time.Second
	sleepTime = 250 * time.Millisecond
)

var L *log.Logger

// main searches for peers using rendezvous and kademlia and makes use of ipfs bootstrap servers to have nodes behind NAT connect to each other.
// Then when the nodes find each other (can take up to 5 min), it should try to upgrade the relayed connections to direct connections using holepunching
// PROBLEM: But the holepunching part does not work for me.. (within 1 second of getting relayed connection and trying to upgrade connection it fails saying no good addresses and "dial refused because of black hole" a few times)


// Testing setup: Node A runs this code in my private network, node B runs on another machine that uses usb-tethered mobile network from my phone to connect to node A.
// Result: The nodes find each other via relayed connection, but the direct connection upgrade fails no matter which timeout I put..

// BTW: "websocket: failed to close network connection" is a known libp2p issue that can be ignored
// And why do you want direct connections? PubSub messages are not distributed if you only have relayed connection.. (this is just a minimal example)

// Tested using currently newest go version: go1.22.3
func main() {
	// set up logger
	L = log.New(os.Stdout,
		"INFO: ",
		log.Ldate|log.Ltime)

	// generate key pair
	var err error
	priv, _, err := crypto.GenerateKeyPair(
		crypto.Ed25519,
		-1,
	)
	if err != nil {
		L.Panic(err)
	}

	// use ifps team hosted bootstrap servers (https://github.com/ipfs/kubo/blob/master/config/bootstrap_peers.go#L17)
    var bootstrapPeers []peer.AddrInfo
	for _, multiAddr := range dht.DefaultBootstrapPeers {
		// convert each multiaddr.MultiAddr to a peer.AddrInfo
		addrInfo, err := peer.AddrInfoFromP2pAddr(multiAddr)
		if err != nil {
			L.Panicf("failed to convert MultiAddr to AddrInfo: %v", err)
		}
		bootstrapPeers = append(bootstrapPeers, *addrInfo)
	}

    // configure node
	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(
				"/ip4/0.0.0.0/tcp/0",
				"/ip4/0.0.0.0/udp/9002/quic-v1",
				"/ip6/::/udp/9002/quic-v1",
		),
		libp2p.Security(libp2ptls.ID, libp2ptls.New),	// support TLS
		libp2p.Security(noise.ID, noise.New),			// support Noise
		libp2p.DefaultTransports,						// support any default transports (includes quic)
        // punch through firewall with help of ipfs hosted relay servers
        libp2p.NATPortMap(),
        libp2p.EnableHolePunching(),
        libp2p.EnableAutoRelayWithStaticRelays(bootstrapPeers),
        libp2p.EnableRelay(), // enabled by default but who cares
        libp2p.EnableNATService(),
	)
	if err != nil {
		L.Panic(err)
	}

	L.Printf("My node ID: %v", h.ID().String())
	L.Printf("I am reachable via these addresses: %v", h.Addrs())

	// try to find peers
	ctx := context.Background()
	ch := make(chan bool)
	go DiscoverPeers(ctx, h, ch)
	<-ch

	L.Printf("Everything worked. Terminating.")
}

// DiscoverPeers uses Rendezvous and Kademlia to discover peers, tries to upgrade relayed connections to direct connections using holepunching (but this part does not work for me)
func DiscoverPeers(ctx context.Context, h host.Host, ch chan bool) {
	kademliaDHT := InitDHT(ctx, h)
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)

	// advertise rendezvous string
	dutil.Advertise(ctx, routingDiscovery, RendezvousString)
	L.Printf("Searching for peers...")
	
	// use map to keep track of nodes you have successfully connected to
	myConnectedPeers := make(map[string]bool)

	// always keep looking for new peers
	for {
		// get list of discovered peers
		peerChan, err := routingDiscovery.FindPeers(ctx, RendezvousString)
		if err != nil {
			L.Panic(err)
		}
		// for each discovered peer, try to connect to it if you have not done so already
		for peer := range peerChan {
			// don't try to connect to yourself
			if peer.ID == h.ID() {
				continue
			}

			// don't connect to the same peer multiple times
			if myConnectedPeers[peer.ID.String()] {
                continue
            }

			err := h.Connect(ctx, peer)		// try to connect to peer (either new connection is opened or an error is returned)
			if err != nil { // un-comment out below if you want to get spammed with failed connection attemps
				//L.Printf("Failed connecting to %v due to error: %v", peer.ID.String(), err)	// this happens and can be annoying, so just dont show it
				time.Sleep(sleepTime)
				continue
			} 
			L.Printf("Successfully got relayed connection to node %v\nWill now try to upgrade to direct connection..", peer.ID.String())

			// ---- I NEED HELP WITH THE FOLLOWING PART (it keeps saying that all dials failed due to black hole, then it timeouts in less than 1 sec cuz of no network activity) ----

			// try to establish direct connection (not relayed connection)
    		
			err = holePunchConnect(ctx, h, peer, true)
			if err != nil {
				L.Printf("Failed to upgrade to direct connection: %v", err)
			} else { // Goal: Have it print the below :)
				L.Printf("Successfully holepunched to upgrade to direct connection with %v", peer.ID.String())
				ch <- true
				break
			}

			// ok you successfully connected to new peer, remember this peer (libp2p's who-is-connected-list seems to contain many inactive nodes)
			myConnectedPeers[peer.ID.String()] = true
			L.Printf("Connected to: %v", peer.ID.String())
			L.Printf("List of addresses this node has:")
			for _, address := range peer.Addrs {
				L.Printf("%v", address.String())
			}
			
		} // end of looping over discovered peers

		time.Sleep(sleepTime) // sleep a bit to not overload cpu
	}
}

// holePunchConnect tries to upgrade relayed connection to direct connection
// Mainly taken from here: https://github.com/libp2p/go-libp2p/blob/master/p2p/protocol/holepunch/util.go#L67
func holePunchConnect(ctx context.Context, host host.Host, pi peer.AddrInfo, isClient bool) error {
	holePunchCtx := network.WithSimultaneousConnect(ctx, isClient, "hole-punching")
	forceDirectConnCtx := network.WithForceDirectDial(holePunchCtx, "hole-punching")
	dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, contextTimeout)
	defer cancel()

	if err := host.Connect(dialCtx, pi); err != nil {
		return fmt.Errorf("hole punch attempt with peer %v was unsuccessful: %v", pi.ID, err)
	}

	return nil
}

// InitDHT connects to a few known default bootstrap nodes (they enable distributed node discovery and are the only centralized necessity in kademlia DHT).
func InitDHT(ctx context.Context, h host.Host) *dht.IpfsDHT {
	kademliaDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto))	
	if err != nil {
		L.Panic(err)
	}

	err = kademliaDHT.Bootstrap(ctx)
	if err != nil {
		L.Panic(err)
	}

	var wg sync.WaitGroup
	for _, peerAddr := range dht.DefaultBootstrapPeers {	// connect to known bootstrap nodes [register yourself in the DHT keyspace - will find neighbor nodes]
		peerinfo, _ := peer.AddrInfoFromP2pAddr(peerAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := h.Connect(ctx, *peerinfo)
			if err != nil {
				L.Printf("Bootstrap warning: %v", err)
			}
		}()
	}
	wg.Wait()

	return kademliaDHT
}