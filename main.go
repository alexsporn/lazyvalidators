package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iotaledger/hive.go/ds"
	"github.com/iotaledger/hive.go/runtime/timeutil"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/nodeclient"
)

const (
	nodeURL                    = "https://api.nova-testnet.iotaledger.net"
	committeeUpdateInterval    = 1 * time.Hour
	lazyValidatorCheckInterval = 5 * time.Second
)

func main() {
	client, err := nodeclient.New(nodeURL)
	if err != nil {
		panic(err)
	}

	event, err := client.EventAPI(context.Background())
	if err != nil {
		panic(err)
	}

	if err := event.Connect(context.Background()); err != nil {
		panic(err)
	}

	done := make(chan bool, 1)
	go func() {
		blockChan, sub := event.BlocksValidation()
		if err := sub.Error(); err != nil {
			panic(err)
		}

		committee := ds.NewSet[iotago.AccountID]()

		updateCommittee := func() {
			fmt.Println("Updating committee")
			resp, err := client.Committee(context.Background())
			if err != nil {
				fmt.Println(err.Error())
				return
			}

			committee.Clear()

			for _, member := range resp.Committee {
				_, addr, err := iotago.ParseBech32(member.AddressBech32)
				if err != nil {
					fmt.Println(err.Error())
					continue
				}
				committee.Add(addr.(*iotago.AccountAddress).AccountID())
			}

			fmt.Printf("Committee updated with %d members at epoch %d\n", committee.Size(), resp.Epoch)
		}

		updateCommitteeTicker := timeutil.NewTicker(updateCommittee, committeeUpdateInterval)
		defer updateCommitteeTicker.Shutdown()

		updateCommittee()

		m := ds.NewSet[iotago.AccountID]()

		lazyValidatorCheckTicker := timeutil.NewTicker(func() {
			fmt.Println("Checking for lazy validators")

			lazy := committee.Filter(func(id iotago.AccountID) bool {
				return !m.Has(id)
			})

			if lazy.Size() > 0 {
				fmt.Printf("Found %d lazy validators:\n", lazy.Size())
				_ = lazy.ForEach(func(id iotago.AccountID) error {
					fmt.Println(id.ToAddress().Bech32(client.CommittedAPI().ProtocolParameters().Bech32HRP()))
					return nil
				})
			}

			m.Clear()

		}, lazyValidatorCheckInterval)
		defer lazyValidatorCheckTicker.Shutdown()

		for {
			select {
			case b := <-blockChan:
				m.Add(b.Header.IssuerID)
			default:
			}
		}
	}()

	signalChan := make(chan os.Signal)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-signalChan:
			done <- true
		case err := <-event.Errors:
			fmt.Println(err.Error())
			done <- true
		}
	}()

	<-done
}
