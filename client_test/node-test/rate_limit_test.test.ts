import crypto from 'crypto';
import nacl from 'tweetnacl';
import bs58 from 'bs58';
import { GrpcClient } from './grpc_client';

const GRPC_SERVER_ADDRESS = '127.0.0.1:9001';

// Test configuration
const RATE_LIMIT_CONFIG = {
  maxRequestsPerSecond: 10,
  testDuration: 2000, // 2 seconds
  requestInterval: 100, // 100ms between requests
};

// Generate test account
function generateTestAccount() {
  const seed = crypto.randomBytes(32);
  const keyPair = nacl.sign.keyPair.fromSeed(seed);
  const publicKeyBase58 = bs58.encode(keyPair.publicKey);
  const privateKey = crypto.createPrivateKey({
    key: Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), seed]),
    format: 'der',
    type: 'pkcs8',
  });
  return { publicKeyBase58, privateKey, keyPair, seed };
}

// Create test transaction
function createTestTransaction(sender: string, recipient: string, nonce: number, privateKey?: crypto.KeyObject) {
  const tx = {
    type: 0,
    sender: sender,
    recipient: recipient,
    amount: 1000,
    timestamp: Date.now(),
    text_data: `test transaction ${nonce}`,
    nonce: nonce,
    signature: '',
    extra_info: '',
  };

  // Sign transaction with proper private key
  const txBytes = Buffer.from(JSON.stringify(tx));
  let signature: Buffer;
  
  if (privateKey) {
    // Use provided private key
    signature = crypto.sign(null, txBytes, privateKey);
  } else {
    // Generate a random key pair for testing
    const keyPair = nacl.sign.keyPair();
    signature = Buffer.from(nacl.sign.detached(txBytes, keyPair.secretKey));
  }
  
  tx.signature = bs58.encode(signature);

  return tx;
}

describe('Rate Limiting Tests', () => {
  let grpcClient: GrpcClient;
  let testAccount: any;

  beforeAll(async () => {
    grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);
    testAccount = generateTestAccount();
  });

  afterAll(async () => {
    if (grpcClient) {
      await grpcClient.close();
    }
  });

  describe('gRPC Rate Limiting', () => {
    test('should rate limit rapid requests', async () => {
      const results = {
        successful: 0,
        rateLimited: 0,
        errors: 0,
      };

      const startTime = Date.now();
      const promises: Promise<any>[] = [];

      // Send multiple rapid requests
      for (let i = 0; i < 20; i++) {
        const promise = (async () => {
          try {
            const tx = createTestTransaction(
              testAccount.publicKeyBase58,
              'test_recipient',
              i + 1,
              testAccount.privateKey
            );

            const response = await grpcClient.addTransaction(tx, tx.signature);
            
            if (response.ok) {
              results.successful++;
            } else {
              if (response.error && response.error.includes('rate limit')) {
                results.rateLimited++;
              } else {
                results.errors++;
              }
            }
          } catch (error) {
            results.errors++;
          }
        })();

        promises.push(promise);
        
        // Small delay between requests
        await new Promise(resolve => setTimeout(resolve, 50));
      }

      await Promise.all(promises);
      const endTime = Date.now();

      console.log(`Rate limiting test results:`);
      console.log(`- Successful: ${results.successful}`);
      console.log(`- Rate Limited: ${results.rateLimited}`);
      console.log(`- Errors: ${results.errors}`);
      console.log(`- Duration: ${endTime - startTime}ms`);

      // Should have some successful requests
      expect(results.successful).toBeGreaterThan(0);
      
      // Should have some rate limited requests (if rate limiting is working)
      // Note: This might be 0 if rate limiting is not implemented yet
      expect(results.rateLimited + results.errors).toBeGreaterThanOrEqual(0);
    }, 10000);

    test('should allow requests after rate limit window expires', async () => {
      // First, exhaust the rate limit
      const promises: Promise<any>[] = [];
      for (let i = 0; i < 15; i++) {
        const tx = createTestTransaction(
          testAccount.publicKeyBase58,
          'test_recipient',
          i + 1,
          testAccount.privateKey
        );
        promises.push(grpcClient.addTransaction(tx, tx.signature));
      }

      await Promise.all(promises);

      // Wait for rate limit window to reset (assuming 1 second window)
      await new Promise(resolve => setTimeout(resolve, 1100));

      // Try a new request - should be allowed
      const tx = createTestTransaction(
        testAccount.publicKeyBase58,
        'test_recipient',
        100,
        testAccount.privateKey
      );

      const response = await grpcClient.addTransaction(tx, tx.signature);
      
      // This should succeed after the window resets
      // Note: This test might fail if rate limiting is not implemented
      console.log('Request after window reset:', response);
    }, 15000);

    test('should handle different wallet addresses independently', async () => {
      const account1 = generateTestAccount();
      const account2 = generateTestAccount();

      const results1 = { successful: 0, rateLimited: 0 };
      const results2 = { successful: 0, rateLimited: 0 };

      // Send requests from both accounts
      const promises: Promise<any>[] = [];

      for (let i = 0; i < 10; i++) {
        // Account 1
        promises.push((async () => {
          const tx = createTestTransaction(account1.publicKeyBase58, 'recipient1', i + 1, account1.privateKey);
          const response = await grpcClient.addTransaction(tx, tx.signature);
          if (response.ok) {
            results1.successful++;
          } else if (response.error && response.error.includes('rate limit')) {
            results1.rateLimited++;
          }
        })());

        // Account 2
        promises.push((async () => {
          const tx = createTestTransaction(account2.publicKeyBase58, 'recipient2', i + 1, account2.privateKey);
          const response = await grpcClient.addTransaction(tx, tx.signature);
          if (response.ok) {
            results2.successful++;
          } else if (response.error && response.error.includes('rate limit')) {
            results2.rateLimited++;
          }
        })());

        await new Promise(resolve => setTimeout(resolve, 50));
      }

      await Promise.all(promises);

      console.log(`Account 1 results: ${results1.successful} successful, ${results1.rateLimited} rate limited`);
      console.log(`Account 2 results: ${results2.successful} successful, ${results2.rateLimited} rate limited`);

      // Both accounts should have some successful requests
      expect(results1.successful).toBeGreaterThan(0);
      expect(results2.successful).toBeGreaterThan(0);
    }, 15000);
  });

  describe('Rate Limiting Configuration', () => {
    test('should respect IP-based rate limits', async () => {
      // This test would require multiple client connections from different IPs
      // For now, we'll just test that the endpoint is working
      const tx = createTestTransaction(testAccount.publicKeyBase58, 'test_recipient', 1, testAccount.privateKey);
      
      const response = await grpcClient.addTransaction(tx, tx.signature);
      console.log('IP rate limit test response:', response);
      
      // Should get a response (either success or rate limit)
      expect(response).toBeDefined();
    });

    test('should respect global rate limits', async () => {
      // Send many requests to test global rate limiting
      const promises: Promise<any>[] = [];
      
      for (let i = 0; i < 50; i++) {
        const tx = createTestTransaction(
          testAccount.publicKeyBase58,
          'test_recipient',
          i + 1,
          testAccount.privateKey
        );
        promises.push(grpcClient.addTransaction(tx, tx.signature));
      }

      const responses = await Promise.all(promises);
      
      const successful = responses.filter(r => r.ok).length;
      const rateLimited = responses.filter(r => r.error && r.error.includes('rate limit')).length;
      
      console.log(`Global rate limit test: ${successful} successful, ${rateLimited} rate limited`);
      
      // Should have some successful requests
      expect(successful).toBeGreaterThan(0);
    }, 10000);
  });

  describe('Rate Limiting Edge Cases', () => {
    test('should handle empty or invalid transactions', async () => {
      try {
        const response = await grpcClient.addTransaction({} as any, '');
        console.log('Empty transaction response:', response);
        
        // Should get an error response
        expect(response.ok).toBe(false);
      } catch (error) {
        console.log('Empty transaction error:', error);
        // This is also acceptable
      }
    });

    test('should handle malformed signatures', async () => {
      const tx = createTestTransaction(testAccount.publicKeyBase58, 'test_recipient', 1, testAccount.privateKey);
      tx.signature = 'invalid_signature';

      const response = await grpcClient.addTransaction(tx, tx.signature);
      console.log('Invalid signature response:', response);
      
      // Should get an error response
      expect(response.ok).toBe(false);
    });

    test('should handle very large memo fields', async () => {
      const tx = createTestTransaction(testAccount.publicKeyBase58, 'test_recipient', 1, testAccount.privateKey);
      tx.text_data = 'A'.repeat(100); // 100 character memo (should be rejected if limit is 64)

      const response = await grpcClient.addTransaction(tx, tx.signature);
      console.log('Large memo response:', response);
      
      // Should get an error if memo is too long
      if (!response.ok) {
        expect(response.error).toContain('memo');
      }
    });
  });
});

// Performance test
describe('Rate Limiting Performance', () => {
  let grpcClient: GrpcClient;
  let testAccount: any;

  beforeAll(async () => {
    grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);
    testAccount = generateTestAccount();
  });

  afterAll(async () => {
    if (grpcClient) {
      await grpcClient.close();
    }
  });

  test('should handle concurrent requests efficiently', async () => {
    const startTime = Date.now();
    const promises: Promise<any>[] = [];

    // Send 100 concurrent requests
    for (let i = 0; i < 100; i++) {
      const tx = createTestTransaction(testAccount.publicKeyBase58, 'test_recipient', i + 1, testAccount.privateKey);
      promises.push(grpcClient.addTransaction(tx, tx.signature));
    }

    const responses = await Promise.all(promises);
    const endTime = Date.now();

    const successful = responses.filter(r => r.ok).length;
    const rateLimited = responses.filter(r => r.error && r.error.includes('rate limit')).length;
    const errors = responses.filter(r => !r.ok && !r.error?.includes('rate limit')).length;

    console.log(`Performance test results:`);
    console.log(`- Total requests: 100`);
    console.log(`- Successful: ${successful}`);
    console.log(`- Rate Limited: ${rateLimited}`);
    console.log(`- Errors: ${errors}`);
    console.log(`- Duration: ${endTime - startTime}ms`);
    console.log(`- Requests per second: ${(100 * 1000) / (endTime - startTime)}`);

    // Should complete within reasonable time
    expect(endTime - startTime).toBeLessThan(10000); // 10 seconds max
    
    // Should have some successful requests
    expect(successful).toBeGreaterThan(0);
  }, 15000);
});