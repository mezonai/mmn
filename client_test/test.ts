import axios from "axios";
import { ethers } from "ethers";
const wallet = new ethers.Wallet(// test address
  "214151f5ec648cac92d0e40998206502fd7dd9bc7e73a049fa66ab8362535a29"
);
const toAddress = "0x35Ea12C82B9335f60db2AFE2D347EecD6B34C316";
const API_URL = "http://localhost:8080"
async function signTx() {
  // Sender    string  `json:"sender"`
	// Recipient string  `json:"recipient"`
	// Amount    big.Int `json:"amount"`
	// Timestamp big.Int `json:"timestamp"`
	// Nonce     big.Int `json:"nonce,omitempty"` // Optional nonce to prevent replay.
  const tx = {
    nonce: Date.now(),
    to: toAddress,
    value: ethers.parseEther("1.15"),
  };

  const signedTx = await wallet.signTransaction(tx);
  console.log("Signed Tx:", signedTx);

  const response = await axios.post(`${API_URL}/sendRawTransaction`, {
      rawData: signedTx
    });
  console.log(`Response`, response.data);
  // wait 1s
  await new Promise((r, rj) => setTimeout(r, 1000))
  const resBalance = await axios.post(`${API_URL}/balance?address=${toAddress}`, {
      rawData: signedTx
    });
  console.log(`resBalance`, resBalance.data);
}

signTx().catch(console.error);
