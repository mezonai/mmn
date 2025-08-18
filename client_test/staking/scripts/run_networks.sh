#!/bin/bash
### Bootstrap Node
./bin/mmn bootnode --privkey-path "./config/bootnode_privkey.txt" --bootstrap-p2p-port 9000
### Node 1
./bin/mmn node --privkey-path "config/validator1_key.txt" --grpc-addr ":9101" --genesis-path config/genesis_with_staking.yml --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"
### Node 2
./bin/mmn node --privkey-path "config/validator2_key.txt" --grpc-addr ":9102" --genesis-path config/genesis_with_staking.yml --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"
### Node 3
./bin/mmn node --privkey-path "config/validator3_key.txt" --grpc-addr ":9103" --genesis-path config/genesis_with_staking.yml --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"