package main

import (
	"fmt"
)

func usage() {
	fmt.Println("microci accepts the following environment variables:")
	fmt.Println("Required")
	fmt.Println(" - MICROCI_GITEA_URL        URL of gitea instance")
	fmt.Println(" - MICROCI_GITEA_SECRETKEY  shared key between microci and gitea")
	fmt.Println(" - MICROCI_GITEA_TOKEN      token used when connecting to gitea (can be replaced by username/password below)")
	fmt.Println("Optional")
	fmt.Println(" - MICROCI_PORT             port to bind webhook listener to (defaults to 80)")
	fmt.Println(" - MICROCI_ADDRESS          address to bind webhook listener to (defaults to all available addresses)")
	fmt.Println(" - MICROCI_GITEA_USERNAME   username used when connecting to gitea")
	fmt.Println(" - MICROCI_GITEA_PASSWORD   password used when connecting to gitea")
}
