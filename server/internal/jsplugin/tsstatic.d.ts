export type Json = null | boolean | number | string | Json[] | { [key: string]: Json };

// ---- hook return values ----

/** Return exactly one of request, result, or error. */
export type ProbeDecision =
  | { request: ProbeRequest; result?: never; error?: never }
  | { result: ProbeResult; request?: never; error?: never }
  | { error: string; request?: never; result?: never };

export type ProtocolDecodeResult =
  | { model: string; error?: never }
  | { error: { status: number; code: string; message: string }; model?: never };

export interface ProtocolParsedResponse {
  /** Send the upstream body to the client unchanged. */
  passthrough?: boolean;
  status?: number;
  contentType?: string;
  body?: string;
  usage?: ProtocolUsage;
  error?: string;
}

// ---- host API v1 (globals) ----

declare global {
  const codec: {
    base64Encode(value: string): string;
    base64Decode(value: string): string;
    base64URLEncode(value: string): string;
    base64URLDecode(value: string): string;
  };
  const crypto: {
    hmacSHA256(key: string, message: string): string;
    sha256(message: string): string;
  };
  const jwt: {
    signHS256(payload: Record<string, any>, secret: string): string;
    decodeHS256(token: string, secret: string): any;
  };
  const utils: { uuid(): string };
  const log: {
    debug(message: string, fields?: Record<string, any>): void;
    info(message: string, fields?: Record<string, any>): void;
    warn(message: string, fields?: Record<string, any>): void;
  };
}
