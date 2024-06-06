package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"os/signal"
	"syscall"

	// libp2p
	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

const (
	RendezvousString = "test18471392328571" // if you feel peer discovery takes too long change this, can sometimes help
	contextTimeout = 60 * time.Second
	sleepTime = 250 * time.Millisecond
	version = "0.9.1"

	myTopic = "testtopic29329528"
)

var L *log.Logger

// stop peer discovery after first connection is made for direct chat example
var directChatExample bool
var firstConnectedNodeAddress peer.ID


// USAGE:
// 		export GOLOG_LOG_LEVEL=p2p-holepunch=debug && go run .

// EXPECTATION: run code on two nodes, after a while they find each other and receive each others topic messages
// Scenario 1 (both nodes are in same LAN): it works as expected
// Scenario 2 [sad ending] (the two nodes are not in the same LAN): after a while they find each other, but they never receive each others topic messages. A few (unrelated?) holepunching successes are printed and no holepunching failure. But when I use vole to ping vole shows success (took x ms), but the other node prints holepunching error: "error": "failed to open hole-punching stream: connection failed"

// Is anyone able to find the flaw in scenario 2? Does it work with your setup? My setup is 1 node is using mobile data usb tethered (to not be in same network), the other node uses normal ethernet connection.
func mainPubsubExample() {
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
    ///*
    var bootstrapPeers []peer.AddrInfo
	for _, multiAddr := range dht.DefaultBootstrapPeers {
		// convert each multiaddr.MultiAddr to a peer.AddrInfo
		addrInfo, err := peer.AddrInfoFromP2pAddr(multiAddr)
		if err != nil {
			L.Panicf("failed to convert MultiAddr to AddrInfo: %v", err)
		}
		bootstrapPeers = append(bootstrapPeers, *addrInfo)
	}
	//*/

    // configure node
	h, err := libp2p.New(
		libp2p.Identity(priv),							   // use generated private key as identity
		libp2p.UserAgent(fmt.Sprintf("pouw/%v", version)), // set agent version so that other nodes know which version of this software this node is running
		
		// options below make node publicly available if possible
		// 		rust tool to verify e.g.: libp2p-lookup dht --network ipfs --peer-id <nodeid> , then it shows the agent version among other listen addresses and supported protocols
		libp2p.EnableAutoRelayWithStaticRelays(bootstrapPeers), // become publicly available via ipfs hosted bootstrap nodes that act as relay servers
		libp2p.EnableHolePunching(), 	// enable holepunching
        libp2p.NATPortMap(), 			// try to use upnp to open port in firewall

        // ---- this will be used implicitely ----
        /*
		libp2p.ListenAddrStrings( // as specified in defaults.go
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
			"/ip4/0.0.0.0/udp/0/quic-v1/webtransport",
			"/ip6/::/tcp/0",
			"/ip6/::/udp/0/quic-v1",
			"/ip6/::/udp/0/quic-v1/webtransport",
		),
		*/

        //libp2p.DefaultSecurity,							// support TLS and Noise
		//libp2p.DefaultTransports,						// support any default transports (tcp, quic, websocket and webtransport)
		//libp2p.DefaultMuxers, 							// support yamux
		//libp2p.DefaultPeerstore, 						// use default peerstore (probably not necessary)

		// ---- tried these but not necessary afaik ----

        //libp2p.EnableRelayService(), 	// if you are publicly available act as a relay and help others behind NAT
        //libp2p.EnableNATService(), 	// if you are publicly available help other to figure out whether they are publicly available or not

        //libp2p.EnableRelay(), 			// makes no difference (because it is enabled by default)
        //libp2p.ForceReachabilityPublic(), // do NOT use this when you are behind NAT, i can't even ping my node with this enabled
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
	

	// use gossipsub for message distribution over topics, returns *pubsub.PubSub (using default GossipSubRouter as the router)
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		L.Panic(err)
	}

	// get topic handler
	myTopicHandler, err := ps.Join(myTopic)
	if err != nil {
		L.Panic(err)
	}

	// subscribe to topic
	sub, err := myTopicHandler.Subscribe()
	if err != nil {
		L.Panic(err)
	}
	L.Printf("Successfully subscribed to topic: %v\n", myTopic)

	// listen to topic messages
	go TopicReceiveMessage(ctx, sub, h)	

	// send message below to topic in regular intervals
	spamThisTopicMessage := fmt.Sprintf("hi topic enjoyers, my peer id is %v", myNodeID)
	go TopicSendMessage(ctx, spamThisTopicMessage, myTopicHandler)

	// try to find peers
	DiscoverPeers(ctx, h)
	<-ctx.Done() // run until manually cancelled

	// run code with two nodes and just wait a few min, as soon as the the nodes find each other you start seeing their topic messages
}

func mainChatExample() {
	directChatExample = true

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
    ///*
    var bootstrapPeers []peer.AddrInfo
	for _, multiAddr := range dht.DefaultBootstrapPeers {
		// convert each multiaddr.MultiAddr to a peer.AddrInfo
		addrInfo, err := peer.AddrInfoFromP2pAddr(multiAddr)
		if err != nil {
			L.Panicf("failed to convert MultiAddr to AddrInfo: %v", err)
		}
		bootstrapPeers = append(bootstrapPeers, *addrInfo)
	}
	//*/

    // configure node
	h, err := libp2p.New(
		libp2p.Identity(priv),							   // use generated private key as identity
		libp2p.UserAgent(fmt.Sprintf("pouw/%v", version)), // set agent version so that other nodes know which version of this software this node is running
		
		// options below make node publicly available if possible
		// 		rust tool to verify e.g.: libp2p-lookup dht --network ipfs --peer-id <nodeid> , then it shows the agent version among other listen addresses and supported protocols
		libp2p.EnableAutoRelayWithStaticRelays(bootstrapPeers), // become publicly available via ipfs hosted bootstrap nodes that act as relay servers
		libp2p.EnableHolePunching(), 	// enable holepunching
        libp2p.NATPortMap(), 			// try to use upnp to open port in firewall

        // ---- this will be used implicitely ----
        /*
		libp2p.ListenAddrStrings( // as specified in defaults.go
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
			"/ip4/0.0.0.0/udp/0/quic-v1/webtransport",
			"/ip6/::/tcp/0",
			"/ip6/::/udp/0/quic-v1",
			"/ip6/::/udp/0/quic-v1/webtransport",
		),
		*/

        //libp2p.DefaultSecurity,							// support TLS and Noise
		//libp2p.DefaultTransports,						// support any default transports (tcp, quic, websocket and webtransport)
		//libp2p.DefaultMuxers, 							// support yamux
		//libp2p.DefaultPeerstore, 						// use default peerstore (probably not necessary)

		// ---- tried these but not necessary afaik ----

        //libp2p.EnableRelayService(), 	// if you are publicly available act as a relay and help others behind NAT
        //libp2p.EnableNATService(), 	// if you are publicly available help other to figure out whether they are publicly available or not

        //libp2p.EnableRelay(), 			// makes no difference (because it is enabled by default)
        //libp2p.ForceReachabilityPublic(), // do NOT use this when you are behind NAT, i can't even ping my node with this enabled
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

    // set stream handler for direct communication via chat protocol (here you define what should happen when someone tries to /chat/ with you)
	h.SetStreamHandler("/chat/1.0", func(stream network.Stream) {
        go ChatReceive(stream, h, ctx)
    })
    L.Printf("Stream handler has been set.")

	// connect to one peer (program will not continue until this happens)
	DiscoverPeers(ctx, h)
	L.Printf("Peer discovery complete. Will start sending chat messages.")

    // repeatedly send your node ID as chat message to the first peer you connected to
	go ChatSendRepeatedly([]byte(myNodeID), firstConnectedNodeAddress, h, ctx)

	select {} // keep running

	// Note: sometimes node A connects to node B and then starts sending msgs that B receives, but B has no realized that it is connected to A and will not start sending messages until it does realize it is connected. Why this realization happens not instantly is beyond me (one would think that being connected is a biconditional).
}

func mainMinimalExample() {
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
    ///*
    var bootstrapPeers []peer.AddrInfo
	for _, multiAddr := range dht.DefaultBootstrapPeers {
		// convert each multiaddr.MultiAddr to a peer.AddrInfo
		addrInfo, err := peer.AddrInfoFromP2pAddr(multiAddr)
		if err != nil {
			L.Panicf("failed to convert MultiAddr to AddrInfo: %v", err)
		}
		bootstrapPeers = append(bootstrapPeers, *addrInfo)
	}
	//*/

    // configure node
	h, err := libp2p.New(
		libp2p.Identity(priv),							   // use generated private key as identity
		libp2p.UserAgent(fmt.Sprintf("pouw/%v", version)), // set agent version so that other nodes know which version of this software this node is running
		
		// options below make node publicly available if possible
		// 		rust tool to verify e.g.: libp2p-lookup dht --network ipfs --peer-id <nodeid> , then it shows the agent version among other listen addresses and supported protocols
		libp2p.EnableAutoRelayWithStaticRelays(bootstrapPeers), // become publicly available via ipfs hosted bootstrap nodes that act as relay servers
		libp2p.EnableHolePunching(), 	// enable holepunching
        libp2p.NATPortMap(), 			// try to use upnp to open port in firewall

        // ---- this will be used implicitely ----
        /*
		libp2p.ListenAddrStrings( // as specified in defaults.go
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
			"/ip4/0.0.0.0/udp/0/quic-v1/webtransport",
			"/ip6/::/tcp/0",
			"/ip6/::/udp/0/quic-v1",
			"/ip6/::/udp/0/quic-v1/webtransport",
		),
		*/

        //libp2p.DefaultSecurity,							// support TLS and Noise
		//libp2p.DefaultTransports,						// support any default transports (tcp, quic, websocket and webtransport)
		//libp2p.DefaultMuxers, 							// support yamux
		//libp2p.DefaultPeerstore, 						// use default peerstore (probably not necessary)

		// ---- tried these but not necessary afaik ----

        //libp2p.EnableRelayService(), 	// if you are publicly available act as a relay and help others behind NAT
        //libp2p.EnableNATService(), 	// if you are publicly available help other to figure out whether they are publicly available or not

        //libp2p.EnableRelay(), 			// makes no difference (because it is enabled by default)
        //libp2p.ForceReachabilityPublic(), // do NOT use this when you are behind NAT, i can't even ping my node with this enabled
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
	<-ctx.Done() // run until manually cancelled

	// run code with two nodes and just wait a few min, they should find each other
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
	outerLoop: // break label for directchat example
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

				// ok you successfully connected to new peer, remember this peer (libp2p's who-is-connected-list seems to contain many inactive nodes)
				myConnectedPeers[peer.ID.String()] = true
				L.Printf("Connected to: %v", peer.ID.String())
				if directChatExample {
					firstConnectedNodeAddress = peer.ID
					break outerLoop
				}


				//L.Printf("List of addresses this node has:")
				//for _, address := range peer.Addrs {
				//	L.Printf("%v", address.String())
				//}


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

// InitDHT connects to a few known default bootstrap nodes (they enable distributed node discovery and are the only centralized necessity in kademlia DHT).
func InitDHT(ctx context.Context, h host.Host) *dht.IpfsDHT {
	kademliaDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto))
	//kademliaDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeAutoServer))	// My Testing: ModeServer -> NATed Node can't be reached at all, ModeAutoServer -> 
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

// ---- Topic stuff ----

// TopicReceiveMessage is responsible for send messages to the topic (sends every 5 seconds).
func TopicSendMessage(ctx context.Context, msgContent string, targetTopic *pubsub.Topic) {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {

		err := targetTopic.Publish(ctx, []byte(msgContent))
		if err != nil {
			L.Panicf("Failed to send msg to topic due to: %v\n", err)
		} else {
			L.Printf("Successfully sent message to topic")
		}

	}
}

// TopicReceiveMessage is responsible for receiving topic messages (checks every 5 ms).
func TopicReceiveMessage(ctx context.Context, sub *pubsub.Subscription, h host.Host) {
	ticker := time.NewTicker(5 * time.Millisecond)

	for range ticker.C {
		m, err := sub.Next(ctx)
		if err != nil {
			L.Panic(err)
		}

		// in a simple 2 node demonstration this helps filter out messages you have received that you have sent yourself. but in more complex example you need to find better approach as ReceivedFrom only means who forwarded the message to you, it might or might not be the original sender and with many nodes you might get your own message back (gossiping aspect of gossipsub)
		if m.ReceivedFrom.String() == h.ID().String() {
			L.Printf("Received topic message that was sent by myself")
			continue
		}

		t := sub.Topic()
		L.Printf("Topic: %v - Received message: %v\n", t, string(m.Data))

	}
}

// ChatSendRepeatedly sends your nodeID directly to another peer via /chat/ protocol repeatedly (ever 5 seconds).
func ChatSendRepeatedly(data []byte, targetPeer peer.ID, h host.Host, ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		chatStream, err := h.NewStream(ctx, targetPeer, "/chat/1.0")
	    if err != nil {
	        L.Panicf("SendViaChat - ERROR: Failed to connect to target peer %v. Will NOT send the message due to: %v\n", targetPeer.String(), err)
	    }

		// send data to the target peer
		_, err = chatStream.Write(data)

		L.Printf("Successfully sent my peerID to node %v via direct chat.", targetPeer.String())

		err = chatStream.Close()
		if err != nil {
			L.Printf("Error closing stream: %v", err)
		}

	}

	
}

// ChatReceive handles incoming chat protocol messages (just prints them). Will be called automatically after setting up streamhandler.
func ChatReceive(chatStream network.Stream, h host.Host, ctx context.Context) {
	defer chatStream.Close()

	senderPeerID := chatStream.Conn().RemotePeer()
	senderNodeID := senderPeerID.String()

	reader := bufio.NewReader(chatStream)
	receivedMessage, err := io.ReadAll(reader)
	if err != nil {
		L.Panicf("HandleIncomingChatMessage - Error reading message: %v. This message was sent by node %v\n", err, senderNodeID)
	}

	L.Printf("Received /chat/ message from node %v with content: %v\n", senderNodeID, string(receivedMessage))
}

// choose which example to run
func main() {
	//mainMinimalExample()
	mainPubsubExample()
	//mainChatExample()
}
