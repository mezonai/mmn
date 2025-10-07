import { ZkClientConfig, IZkProof } from './types';

export class ZkClient {
  private endpoint: string;
  private timeout: number;
  private headers: Record<string, string>;

  constructor(config: ZkClientConfig) {
    this.endpoint = config.endpoint;
    this.timeout = config.timeout || 30000;
    this.headers = {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      ...(config.headers || {}),
    };
  }

  private async makeRequest<T>(
    method: 'GET' | 'POST',
    path: string,
    params?: Record<string, string | number>,
    body?: any
  ): Promise<T> {
    let url = `${this.endpoint}/${path}`;

    // Add query parameters for GET requests
    if (params && Object.keys(params).length > 0) {
      const searchParams = new URLSearchParams();
      Object.entries(params).forEach(([key, value]) => {
        searchParams.append(key, String(value));
      });
      url += `?${searchParams.toString()}`;
    }

    // Create AbortController for timeout
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), this.timeout);

    try {
      const requestOptions: RequestInit = {
        method,
        mode: 'cors',
        credentials: 'omit',
        signal: controller.signal,
        headers: this.headers,
      };

      // Add body for POST requests
      if (method === 'POST' && body) {
        requestOptions.body = JSON.stringify(body);
      }

      const response = await fetch(url, requestOptions);
      clearTimeout(timeoutId);

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      const data = await response.json();
      return data as T;
    } catch (error) {
      clearTimeout(timeoutId);
      if (error instanceof Error) {
        if (error.name === 'AbortError') {
          throw new Error(`Request timeout after ${this.timeout}ms`);
        }
        throw error;
      }
      throw new Error('Request failed');
    }
  }

  public async getZkProofs({
    userId,
    ephemeralPublicKey,
    jwt,
    address,
  }: {
    userId: string;
    ephemeralPublicKey: string;
    jwt: string;
    address: string;
  }): Promise<IZkProof> {
    const path = `prove`;
    const res = await this.makeRequest<{ data: IZkProof }>(
      'POST',
      path,
      undefined,
      {
        user_id: userId,
        ephemeral_pk: ephemeralPublicKey,
        jwt,
        address,
      }
    );

    return res.data;
  }
}
