# golibp2p-holepunchingTest
Testing go-libp2p holepunching with minimal example and some more info for veryifing that holepunching can work.

## Problem main.go
A node run by this is able to be holepunched by other nodes (that I do not control). However, you can not run a node with this code to holepunch someone else (so the logic for initiating the holepunch in this code seems to be wrong, but a node run by this code is able to get holepunched by other tools). Also when using vole with another node the node you target will print holepunch failures, but why are other nodes able to holepunch? What am I doing wrong?

## Problem gossipsubFailureExample.go
The code works perfectly when both nodes are in same LAN. But when one node is in a different network the nodes find each other and connect to each other but they never are able to receive each others gossip sub messages. What am I doing wrong? It probably is related to the holepunch error that is logged when you vole ping one of the nodes..
