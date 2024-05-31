package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"os/signal"
    "syscall"

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
	version = "0.9.1" 
)

var L *log.Logger


// USAGE:
// 		export GOLOG_LOG_LEVEL=p2p-holepunch=debug && go run .

// main searches for peers using rendezvous and kademlia and makes use of ipfs bootstrap servers to have nodes behind NAT connect to each other.
// Then when the nodes find each other (can take up to 5 min), it should try to upgrade the relayed connections to direct connections using holepunching
// PROBLEM: But the holepunching part does not work for me.. It seems to pretend that dcutr (direct connectio upgrade) protocol is not supported, but it is!
// So i know that my network supports holepunching and that this program runs a node that will successfully ACCEPT holepunches, but it fails to INITIATE the holepunches?

// Testing setup: Node A runs this code in my private network, node B runs on another machine that uses usb-tethered mobile network from my phone to connect to node A.
// Result: The nodes find each other via relayed connection, and holepunching via tools such as vole works, but if you run this go code on another machine outside your network the holepunching fails.. so how to do holepunching with go-libp2p correctly? I hoped it would happen automatically

// Tested using currently newest go version: go1.22.3

// To look up addresses of a peer by its ID (and ensure that it's publicly reachable) you can use at least 2 ways:
//    1. install ipfs, start daemon using 'ipfs daemon', then run:   ipfs routing findpeer <nodeID>
//    2. install rust and cargo install libp2p-lookup, then run: libp2p-lookup dht --network ipfs --peer-id <nodeID> [if it says thread 'main' panicked, then wait 2 minutes and try again]
// IMPORTANT: you must append /p2p/<yourNodeID> when you want to try to connect to your node

// To test whether holepunching works and see that your node is publicly available and supports the direct connection upgrade:
//		1. build vole [https://github.com/ipfs-shipyard/vole] using: go install github.com/ipfs-shipyard/vole@latest
//		2. run: 	cd "$(go env GOPATH)/bin" && vole libp2p ping <address> [here address is e.g. /ip4/139.178.65.157/tcp/4001/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa/p2p-circuit/p2p/<yourNodeID>]
//			Note: It is important that the address ends with: p2p-circuit/p2p/<value>, so if the output of libp2p-lookup stops with p2p-circuit you must manually append the /p2p/<nodeIDprintedAtstartup>, if it does not work try it with another address.

// BTW: "websocket: failed to close network connection" is a known libp2p issue that can be ignored
// And why do you want direct connections? PubSub messages seem to not get distributed if you only have relayed connection.. (this is just a minimal example)

func main() {
	// set up logger
	L = log.New(os.Stdout, "", log.Ldate|log.Ltime)

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
		libp2p.UserAgent(fmt.Sprintf("pouw/%v", version)), // set agent version so that other nodes know which version of this software this node is running
		// e.g. libp2p-lookup dht --network ipfs --peer-id <nodeid> , then it shows the agent version among other listen addresses and supported protocols

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

	// print information about node
	myNodeID := h.ID().String()
	L.Printf("My node ID: %v", myNodeID)

	/*
	// print node addresses after 3 seconds
	go func() {
		time.Sleep(3 * time.Second)

		L.Printf("I am reachable via these addresses:")
		for _, addr := range h.Addrs() {
			L.Printf("\t%v/p2p/%v", addr, h.ID().String())
		}
	}()
	*/

	// with lots of trial and error i found out that there is one ipfs hosted bootstrap server that usually works for holepunching (many other don't):
	reliableIPFSserver := "/ip4/139.178.65.157/tcp/4001/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa/p2p-circuit"
	fullAddressToThisNode := fmt.Sprintf("%v/p2p/%v", reliableIPFSserver, myNodeID)
	L.Printf("Run the following command on a machine in a different network to verify that holepunching works (if it fails wait a min and try again):\n\tvole libp2p ping %v\n\n", fullAddressToThisNode)
	// on success it will print 3x: 'Took <time>ms', this will prove that holepunching works with your network

	// let user cancel execution by sending SIGINT or SIGTERM (prevents issue where ctrl+C or ctrl+. don't kill running program)
	ctx, cancel := context.WithCancel(context.Background())
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    go func() {
    	<-sigs
        cancel()
    }()
	
	// try to find peers
	DiscoverPeers(ctx, h)
	<-ctx.Done()
}

// DiscoverPeers uses Rendezvous and Kademlia to discover peers, tries to upgrade relayed connections to direct connections using holepunching (but this part does not work for me)
func DiscoverPeers(ctx context.Context, h host.Host) {
	kademliaDHT := InitDHT(ctx, h)
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)

	// advertise rendezvous string
	dutil.Advertise(ctx, routingDiscovery, RendezvousString)
	L.Printf("Searching for peers...")
	
	// use map to keep track of nodes you have successfully connected to
	myConnectedPeers := make(map[string]bool)

	// always keep looking for new peers, but let user cancel search with ctrl+C or ctrl+. or whatever
	for {
		select {
        case <-ctx.Done():
            L.Println("Peer discovery was manually stopped.")
            return
        default:

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

				// ---- I NEED HELP WITH THE FOLLOWING PART (it keeps saying: "error": "failed to open hole-punching stream: failed to negotiate protocol: protocols not supported: [/libp2p/dcutr]") but when i use libp2p-lookup dht --network ipfs --peer-id <nodeID> it tells me that that protocol is supported [and everyone else is able to holepunch too, so this code must be wrong] ----

				// try to establish direct connection (not relayed connection)
	    		
				err = holePunchConnect(ctx, h, peer, true)
				if err != nil {
					L.Printf("Failed to upgrade to direct connection: %v", err)
				} else { // Goal: Have it print the below :)
					L.Printf("Successfully holepunched to upgrade to direct connection with %v", peer.ID.String())
					break
				}

				// ok you successfully connected to new peer, remember this peer (libp2p's who-is-connected-list seems to contain many inactive nodes)
				myConnectedPeers[peer.ID.String()] = true
				L.Printf("Connected to: %v", peer.ID.String())
				L.Printf("List of addresses that node has:")
				for _, address := range peer.Addrs {
					L.Printf("%v", address.String())
				}
				// Note: This list does not seem to be complete and also does not include any relay addresses (that will be used by other nodes to then holepunch upgrade to a direct connection)
				// For instance, it might print:
						// /ip4/127.0.0.1/tcp/52129/p2p/12D3KooWCT6VdL7XEyBZVwNn5PmeFV1BZ9vaiQCGqN5i6qxgTLdB
						// /ip4/192.168.1.251/tcp/52129/p2p/12D3KooWCT6VdL7XEyBZVwNn5PmeFV1BZ9vaiQCGqN5i6qxgTLdB
						// /ip6/::1/udp/9002/quic-v1/p2p/12D3KooWCT6VdL7XEyBZVwNn5PmeFV1BZ9vaiQCGqN5i6qxgTLdB
				// But when you actually lookup your node via libp2p-lookup you might see these addresses:
						//	- "/ip4/192.168.1.251/tcp/52129"
						//	- "/ip4/192.168.100.95/tcp/36525"
						//	- "/ip4/192.168.100.95/udp/41197/quic-v1"
						// .. along with all the relay addresses

				
			} // end of looping over discovered peers

			time.Sleep(sleepTime) // sleep a bit to not overload cpu
		
		} // end of select



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
