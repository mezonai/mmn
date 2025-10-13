import {GrpcTransport} from '@protobuf-ts/grpc-transport';
import {ChannelCredentials} from '@grpc/grpc-js';
import {ITxServiceClient, TxServiceClient} from './generated/tx.client';
import {AccountServiceClient, IAccountServiceClient} from './generated/account.client';
import type {
  AddTxResponse as GenAddTxResponse,
  SignedTxMsg as GenSignedTxMsg,
  SubscribeTransactionStatusRequest as GenSubscribeTxStatusRequest,
  TransactionStatusInfo as GenTxStatusInfo,
  TxMsg as GenTxMsg,
} from './generated/tx';
import {TransactionStatus as GenTxStatusEnum} from './generated/tx';
import type {
  GetAccountRequest as GenGetAccountRequest,
  GetAccountResponse as GenGetAccountResponse,
  GetCurrentNonceRequest as GenGetCurrentNonceRequest,
  GetCurrentNonceResponse as GenGetCurrentNonceResponse,
  GetTxHistoryRequest as GenGetTxHistoryRequest,
  GetTxHistoryResponse as GenGetTxHistoryResponse,
} from './generated/account';

export class GrpcClient {
  private transport: GrpcTransport;
  private txClient: ITxServiceClient;
  private accountClient: IAccountServiceClient;
  private debug: boolean;

  constructor(serverAddress: string, debug: boolean = false) {
    this.transport = new GrpcTransport({
      host: serverAddress,
      channelCredentials: ChannelCredentials.createInsecure(),
    });
    this.txClient = new TxServiceClient(this.transport);
    this.accountClient = new AccountServiceClient(this.transport);
    this.debug = debug;
  }

  async addTransaction(
    txMsg: {
      type: number;
      sender: string;
      recipient: string;
      amount: number;
      timestamp: number;
      text_data: string;
      nonce: number;
      extra_info: string;
      zk_proof: string;
      zk_pub: string;
    },
    signature: string
  ): Promise<{ ok: boolean; tx_hash?: string; error?: string }> {
    const genTx: GenTxMsg = {
      type: txMsg.type,
      sender: txMsg.sender,
      recipient: txMsg.recipient,
      amount: txMsg.amount.toString(),
      timestamp: BigInt(txMsg.timestamp),
      textData: txMsg.text_data,
      nonce: BigInt(txMsg.nonce),
      extraInfo: txMsg.extra_info,
      zkProof: txMsg.zk_proof,
      zkPub: txMsg.zk_pub,
    };
    const req: GenSignedTxMsg = { txMsg: genTx, signature };

    const call = this.txClient.addTx(req);
    const res: GenAddTxResponse = await call.response;
    return { ok: res.ok, tx_hash: res.txHash, error: res.error };
  }

  async getAccount(address: string): Promise<{ address: string; balance: string; nonce: string }> {
    const req: GenGetAccountRequest = { address };
    const call = this.accountClient.getAccount(req);
    const res: GenGetAccountResponse = await call.response;
    return {
      address: res.address,
      balance: res.balance.toString(),
      nonce: res.nonce.toString(),
    };
  }

  async getTxHistory(
    address: string,
    limit: number,
    offset: number,
    filter: number
  ): Promise<{
    total: number;
    txs: { sender: string; recipient: string; amount: string; nonce: string; timestamp: string; status: string, extraInfo?: string }[];
  }> {
    const req: GenGetTxHistoryRequest = { address, limit, offset, filter };
    const call = this.accountClient.getTxHistory(req);
    const res: GenGetTxHistoryResponse = await call.response;
    return {
      total: res.total,
      txs: res.txs.map((tx) => ({
        sender: tx.sender,
        recipient: tx.recipient,
        amount: tx.amount.toString(),
        nonce: tx.nonce.toString(),
        timestamp: tx.timestamp.toString(),
        status: ['PENDING', 'CONFIRMED', 'FINALIZED', 'FAILED'][tx.status] || 'PENDING',
        extraInfo: tx.extraInfo,
      })),
    };
  }

  subscribeTransactionStatus(
    onUpdate: (update: {
      tx_hash: string;
      status: string;
      block_slot?: string;
      block_hash?: string;
      confirmations?: string;
      error_message?: string;
      timestamp?: string;
    }) => void,
    onError: (error: any) => void,
    onComplete: () => void
  ): () => void {
    const req: GenSubscribeTxStatusRequest = {}; // Empty request for all transactions
    const abortController = new AbortController();
    const call = this.txClient.subscribeTransactionStatus(req, { abort: abortController.signal });

    (async () => {
      try {
        for await (const update of call.responses as AsyncIterable<GenTxStatusInfo>) {
          // Log the raw update from the server (with BigInt handling)
          const serializableUpdate = {
            txHash: update.txHash,
            status: update.status,
            blockSlot: update.blockSlot?.toString(),
            blockHash: update.blockHash,
            confirmations: update.confirmations?.toString(),
            errorMessage: update.errorMessage,
            timestamp: update.timestamp?.toString(),
          };
          if (this.debug) {
          console.log(`🔄 Raw Update from Server:`, JSON.stringify(serializableUpdate, null, 2));
        }

          const statusStr = GenTxStatusEnum[update.status] as unknown as string;
          const processedUpdate = {
            tx_hash: update.txHash,
            status: statusStr || 'UNKNOWN',
            block_slot: update.blockSlot ? update.blockSlot.toString() : undefined,
            block_hash: update.blockHash || undefined,
            confirmations: update.confirmations ? update.confirmations.toString() : undefined,
            error_message: update.errorMessage || undefined,
            timestamp: update.timestamp ? update.timestamp.toString() : undefined,
          };

          // Log the processed update
          if (this.debug) {
          console.log(`📤 Processing Update:`, JSON.stringify(processedUpdate, null, 2));
        }

          onUpdate(processedUpdate);
        }
        onComplete();
      } catch (err: any) {
        if (abortController.signal.aborted) {
          // treat as completed due to unsubscribe
          onComplete();
          return;
        }
        onError(err);
      }
    })();

    // Return unsubscribe that cancels the streaming call
    return () => {
      try {
        abortController.abort();
      } catch (_e) {
        // ignore
      }
    };
  }

  async getCurrentNonce(address: string, tag: string = 'latest'): Promise<{ address: string; nonce: string; tag: string; error: string }> {
    const req: GenGetCurrentNonceRequest = { address, tag };
    const call = this.accountClient.getCurrentNonce(req);
    const res: GenGetCurrentNonceResponse = await call.response;
    return {
      address: res.address,
      nonce: res.nonce.toString(),
      tag: res.tag,
      error: res.error,
    };
  }

  close(): void {
    this.transport.close();
  }

  setDebug(debug: boolean) {
    this.debug = debug;
  }
}
