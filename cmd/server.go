package cmd

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"log"
	"strconv"

	"github.com/hyperledger/fabric-config/protolator"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/channel"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/ledger"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/resmgmt"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/context"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/config"
	"github.com/hyperledger/fabric-sdk-go/pkg/fabsdk"
	"github.com/kfsoftware/hlf-api/pkg/blocks"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type ChaincodeInvocation struct {
	Fcn  string   `json:"fcn"`
	Args []string `json:"args"`
}
type serveOptions struct {
	hlfConfig string
	user      string
	address   string
	org       string
}

func getChannelProvider(user string, org string, sdk *fabsdk.FabricSDK, channelID string) context.ChannelProvider {
	return sdk.ChannelContext(
		channelID,
		fabsdk.WithUser(user),
		fabsdk.WithOrg(org),
	)
}
func getChannelContext(user string, org string, sdk *fabsdk.FabricSDK, channelID string) (context.Channel, error) {
	chCtx, err := getChannelProvider(
		user,
		org,
		sdk,
		channelID,
	)()
	if err != nil {
		log.Fatalf("Failed to create the channel client: %s\n", err)
		return nil, err
	}
	return chCtx, nil
}
func getDiscoveryClient(user string, org string, sdk *fabsdk.FabricSDK, channelID string) (fab.DiscoveryService, error) {
	chCtx, err := getChannelContext(user, org, sdk, channelID)
	if err != nil {
		log.Fatalf("Failed to create the channel client: %s\n", err)
		return nil, err
	}
	discovery, err := chCtx.ChannelService().Discovery()
	if err != nil {
		log.Fatalf("Failed to create the channel client: %s\n", err)
		return nil, err
	}
	return discovery, nil
}

func newServerCmd() *cobra.Command {
	srvOptions := serveOptions{}
	cmd := &cobra.Command{
		Use: "serve",
		RunE: func(cmd *cobra.Command, args []string) error {
			println("Server startup")
			r := gin.Default()
			configBackend := config.FromFile(srvOptions.hlfConfig)
			sdk, err := fabsdk.New(configBackend)
			if err != nil {
				return err
			}
			r.Use(cors.New(cors.Config{
				AllowAllOrigins:  true,
				AllowMethods:     []string{"GET", "PUT", "POST"},
				AllowCredentials: true,
			}))
			r.GET("/channels/:channelId", func(c *gin.Context) {
				channelId := c.Param("channelId")
				chClient := getChannelProvider(srvOptions.user, srvOptions.org, sdk, channelId)
				ledgerClient, err := ledger.New(chClient)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				info, err := ledgerClient.QueryInfo()
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				c.IndentedJSON(200, gin.H{
					"status":            "SUCCESS",
					"height":            info.BCI.Height,
					"currentBlockHash":  hex.EncodeToString(info.BCI.CurrentBlockHash),
					"previousBlockHash": hex.EncodeToString(info.BCI.PreviousBlockHash),
					"endorser":          info.Endorser,
					"heightStatus":      info.Status,
				})
			})
			r.GET("/peers/:peerId/chaincodes", func(c *gin.Context) {
				peerId := c.Param("peerId")
				euipoClientCtx := sdk.Context(fabsdk.WithUser(srvOptions.user), fabsdk.WithOrg(srvOptions.org))
				clientProvider, err := resmgmt.New(euipoClientCtx)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				ccs, err := clientProvider.LifecycleQueryInstalledCC(
					resmgmt.WithTargetEndpoints(peerId),
				)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				c.IndentedJSON(200, gin.H{
					"status":     "SUCCESS",
					"chaincodes": ccs,
				})
			})
			r.GET("/channels/:channelId/chaincodes", func(c *gin.Context) {
				channelId := c.Param("channelId")
				euipoClientCtx := sdk.Context(fabsdk.WithUser(srvOptions.user), fabsdk.WithOrg(srvOptions.org))
				clientProvider, err := resmgmt.New(euipoClientCtx)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				ccs, err := clientProvider.LifecycleQueryCommittedCC(
					channelId,
					resmgmt.LifecycleQueryCommittedCCRequest{},
				)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				c.IndentedJSON(200, gin.H{
					"status":     "SUCCESS",
					"chaincodes": ccs,
				})
			})
			r.GET("/channels/:channelId/blocks/:blockNumber", func(c *gin.Context) {
				channelId := c.Param("channelId")
				blockNumberStr := c.Param("blockNumber")
				blockNumber, err := strconv.Atoi(blockNumberStr)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				chClient := getChannelProvider(srvOptions.user, srvOptions.org, sdk, channelId)
				ledgerClient, err := ledger.New(chClient)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				block, err := blocks.GetBlock(ledgerClient, blockNumber)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				c.IndentedJSON(200, gin.H{
					"status": "SUCCESS",
					"block":  block,
				})
			})
			r.POST("/channels/:channelId/chaincode/:chaincodeId/invoke", func(c *gin.Context) {
				channelId := c.Param("channelId")
				chaincodeId := c.Param("chaincodeId")

				chProvider := getChannelProvider(srvOptions.user, srvOptions.org, sdk, channelId)
				chClient, err := channel.New(chProvider)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				var body ChaincodeInvocation
				err = c.BindJSON(&body)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				var args [][]byte
				for _, arg := range body.Args {
					args = append(args, []byte(arg))
				}
				execReponse, err := chClient.Execute(
					channel.Request{
						ChaincodeID:     chaincodeId,
						Fcn:             body.Fcn,
						Args:            args,
						TransientMap:    nil,
						InvocationChain: nil,
						IsInit:          false,
					},
				)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				c.IndentedJSON(200, gin.H{
					"status": "SUCCESS",
					"block":  execReponse,
				})
			})

			r.GET("/channels/:channelId/config", func(c *gin.Context) {
				channelId := c.Param("channelId")
				chClient := getChannelProvider(srvOptions.user, srvOptions.org, sdk, channelId)
				ledgerClient, err := ledger.New(chClient)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				configBlock, err := ledgerClient.QueryConfigBlock()
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				var buffer bytes.Buffer
				err = protolator.DeepMarshalJSON(&buffer, configBlock)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				var v interface{}
				err = json.Unmarshal(buffer.Bytes(), &v)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				c.Data(200, "application/json", buffer.Bytes())
			})
			r.POST("/channels/:channelId/chaincode/:chaincodeId/query", func(c *gin.Context) {
				channelId := c.Param("channelId")
				chaincodeId := c.Param("chaincodeId")

				chProvider := getChannelProvider(srvOptions.user, srvOptions.org, sdk, channelId)
				chClient, err := channel.New(chProvider)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				var body ChaincodeInvocation
				err = c.BindJSON(&body)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				var args [][]byte
				for _, arg := range body.Args {
					args = append(args, []byte(arg))
				}
				execReponse, err := chClient.Query(
					channel.Request{
						ChaincodeID:     chaincodeId,
						Fcn:             body.Fcn,
						Args:            args,
						TransientMap:    nil,
						InvocationChain: nil,
						IsInit:          false,
					},
				)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				c.IndentedJSON(200, gin.H{
					"status": "SUCCESS",
					"block":  execReponse,
				})
			})
			r.GET("/channels/:channelId/blocks", func(c *gin.Context) {
				channelId := c.Param("channelId")
				fromStr := c.Query("from")
				toStr := c.Query("to")
				reverseStr := c.Query("reverse")
				var reverse bool
				if reverseStr == "1" {
					reverse = true
				}
				chClient := getChannelProvider(srvOptions.user, srvOptions.org, sdk, channelId)
				ledgerClient, err := ledger.New(chClient)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				from, err := strconv.Atoi(fromStr)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				to, err := strconv.Atoi(toStr)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				if from >= to {
					c.AbortWithError(500, errors.Errorf("from is higher than to"))
					return
				}
				totalBlocks := []*blocks.Block{}
				var blockNumbers []int
				if reverse {
					info, err := ledgerClient.QueryInfo()
					if err != nil {
						c.AbortWithError(500, err)
						return
					}
					chHeight := int(info.BCI.Height) - 1
					for i := from; i < to; i++ {
						blkNmbr := chHeight - i
						if blkNmbr >= 0 {
							blockNumbers = append(blockNumbers, blkNmbr)
						}
					}
				} else {
					for blkNmbr := from; blkNmbr < to; blkNmbr++ {
						blockNumbers = append(blockNumbers, blkNmbr)
					}
				}
				for _, blockNumber := range blockNumbers {
					block, err := blocks.GetBlock(ledgerClient, blockNumber)
					if err != nil {
						c.AbortWithError(500, err)
						return
					}
					totalBlocks = append(totalBlocks, block)
					logrus.Debugf("block number %d", block.Number)
				}
				c.IndentedJSON(200, gin.H{
					"status": "SUCCESS",
					"blocks": totalBlocks,
				})
			})
			r.GET("/channels/:channelId/peers", func(c *gin.Context) {
				channelId := c.Param("channelId")
				discovery, err := getDiscoveryClient(srvOptions.user, srvOptions.org, sdk, channelId)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				peers, err := discovery.GetPeers()
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				jsonPeers := []map[string]interface{}{}
				for _, peer := range peers {
					props := peer.Properties()
					ledgerHeight := props[fab.PropertyLedgerHeight]
					jsonPeers = append(jsonPeers, map[string]interface{}{
						"mspID":  peer.MSPID(),
						"url":    peer.URL(),
						"height": ledgerHeight,
					})
				}
				c.IndentedJSON(200, gin.H{
					"status": "SUCCESS",
					"peers":  jsonPeers,
				})
			})
			r.GET("/channels/:channelId/peers/:peerId", func(c *gin.Context) {
				channelId := c.Param("channelId")
				peerId := c.Param("peerId")
				discovery, err := getDiscoveryClient(srvOptions.user, srvOptions.org, sdk, channelId)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				euipoClientCtx := sdk.Context(fabsdk.WithUser(srvOptions.user), fabsdk.WithOrg(srvOptions.org))
				clientProvider, err := resmgmt.New(euipoClientCtx)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				err = clientProvider.JoinChannel(
					channelId,
					resmgmt.WithTargetEndpoints(peerId),
				)
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				peers, err := discovery.GetPeers()
				if err != nil {
					c.AbortWithError(500, err)
					return
				}
				jsonPeers := []map[string]interface{}{}
				for _, peer := range peers {
					props := peer.Properties()
					ledgerHeight := props[fab.PropertyLedgerHeight]
					leftChannel := props[fab.PropertyLeftChannel]
					jsonPeers = append(jsonPeers, map[string]interface{}{
						"mspID":   peer.MSPID(),
						"url":     peer.URL(),
						"height":  ledgerHeight,
						"channel": leftChannel,
					})
				}
				c.IndentedJSON(200, gin.H{
					"status": "SUCCESS",
					"peers":  jsonPeers,
				})
			})
			r.GET("/ping", func(c *gin.Context) {
				c.IndentedJSON(200, gin.H{
					"message": "pong",
				})
			})
			return r.Run(srvOptions.address)
		},
	}
	persistentFlags := cmd.PersistentFlags()
	persistentFlags.StringVarP(&srvOptions.hlfConfig, "hlf-config", "", "", "Configuration file for the SDK")
	persistentFlags.StringVarP(&srvOptions.address, "address", "", "0.0.0.0:8090", "Address for the server")
	persistentFlags.StringVarP(&srvOptions.user, "user", "", "", "User used to interact with the blockchain")
	persistentFlags.StringVarP(&srvOptions.org, "org", "", "", "Organization used to interact with the blockchain")
	cmd.MarkPersistentFlagRequired("hlf-config")
	cmd.MarkPersistentFlagRequired("user")
	cmd.MarkPersistentFlagRequired("org")
	return cmd
}
