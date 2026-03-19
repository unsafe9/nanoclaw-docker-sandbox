import { createServer, IncomingMessage, Server } from 'http';
import crypto from 'crypto';

import { WebSocketServer, WebSocket } from 'ws';

import { ASSISTANT_NAME } from '../config.js';
import { logger } from '../logger.js';
import { Channel, NewMessage, OnInboundMessage, OnChatMetadata } from '../types.js';
import { registerChannel, ChannelOpts } from './registry.js';

const TUI_JID = 'tui:local';
const DEFAULT_PORT = 3333;

// --- WebSocket protocol types ---

interface HelloMsg {
  type: 'hello';
  sender_name?: string;
}

interface ChatMsg {
  type: 'message';
  content: string;
  sender_name?: string;
}

interface HistoryRequestMsg {
  type: 'history_request';
  limit?: number;
}

type InboundWsMsg = HelloMsg | ChatMsg | HistoryRequestMsg;

interface HelloAckMsg {
  type: 'hello_ack';
  session_id: string;
  assistant_name: string;
}

interface OutboundChatMsg {
  type: 'message';
  content: string;
  sender_name: string;
  timestamp: string;
}

interface TypingMsg {
  type: 'typing';
  is_typing: boolean;
}

interface HistoryMsg {
  type: 'history';
  messages: Array<{
    content: string;
    sender_name: string;
    timestamp: string;
    is_bot_message: boolean;
  }>;
}

interface ErrorMsg {
  type: 'error';
  message: string;
}

type OutboundWsMsg =
  | HelloAckMsg
  | OutboundChatMsg
  | TypingMsg
  | HistoryMsg
  | ErrorMsg;

// --- Channel implementation ---

export class TuiChannel implements Channel {
  name = 'tui';

  private server: Server | null = null;
  private wss: WebSocketServer | null = null;
  private clients = new Set<WebSocket>();
  private listening = false;
  private port: number;
  private opts: ChannelOpts;
  private outgoingQueue: Array<{ jid: string; text: string }> = [];
  private flushing = false;
  // In-memory history for reconnection (keeps last N messages)
  private messageHistory: Array<{
    content: string;
    sender_name: string;
    timestamp: string;
    is_bot_message: boolean;
  }> = [];
  private readonly maxHistory = 200;

  constructor(opts: ChannelOpts) {
    this.opts = opts;
    this.port = parseInt(process.env.TUI_WS_PORT || String(DEFAULT_PORT), 10);
  }

  async connect(): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const server = createServer();
      this.server = server;

      this.wss = new WebSocketServer({ server });

      this.wss.on('connection', (ws: WebSocket, _req: IncomingMessage) => {
        this.clients.add(ws);
        logger.info(
          { clientCount: this.clients.size },
          'TUI client connected',
        );

        // Flush queued messages to newly connected client
        this.flushOutgoingQueue().catch((err) =>
          logger.error({ err }, 'Failed to flush TUI outgoing queue'),
        );

        ws.on('message', (data: Buffer) => {
          try {
            const msg = JSON.parse(data.toString()) as InboundWsMsg;
            this.handleClientMessage(ws, msg);
          } catch (err) {
            logger.warn({ err }, 'Invalid TUI WebSocket message');
            this.sendTo(ws, {
              type: 'error',
              message: 'Invalid JSON',
            });
          }
        });

        ws.on('close', () => {
          this.clients.delete(ws);
          logger.info(
            { clientCount: this.clients.size },
            'TUI client disconnected',
          );
        });

        ws.on('error', (err) => {
          logger.warn({ err }, 'TUI WebSocket client error');
          this.clients.delete(ws);
        });
      });

      server.on('error', (err: NodeJS.ErrnoException) => {
        if (err.code === 'EADDRINUSE') {
          logger.warn(
            { port: this.port },
            'TUI port in use, trying next port',
          );
          this.port++;
          server.listen(this.port, '0.0.0.0');
        } else {
          reject(err);
        }
      });

      server.listen(this.port, '0.0.0.0', () => {
        this.listening = true;
        logger.info(
          { port: this.port },
          'TUI WebSocket server listening',
        );
        resolve();
      });
    });
  }

  async sendMessage(jid: string, text: string): Promise<void> {
    const msg: OutboundChatMsg = {
      type: 'message',
      content: text,
      sender_name: ASSISTANT_NAME,
      timestamp: new Date().toISOString(),
    };

    // Track in history
    this.pushHistory({
      content: text,
      sender_name: ASSISTANT_NAME,
      timestamp: msg.timestamp,
      is_bot_message: true,
    });

    if (this.clients.size === 0) {
      this.outgoingQueue.push({ jid, text });
      logger.debug(
        { jid, queueSize: this.outgoingQueue.length },
        'No TUI clients, message queued',
      );
      return;
    }

    this.broadcast(msg);
    logger.info({ jid, length: text.length }, 'TUI message sent');
  }

  isConnected(): boolean {
    return this.listening;
  }

  ownsJid(jid: string): boolean {
    return jid.startsWith('tui:');
  }

  async disconnect(): Promise<void> {
    this.listening = false;
    for (const ws of this.clients) {
      ws.close();
    }
    this.clients.clear();
    this.wss?.close();
    this.server?.close();
    logger.info('TUI channel disconnected');
  }

  async setTyping(jid: string, isTyping: boolean): Promise<void> {
    this.broadcast({ type: 'typing', is_typing: isTyping });
  }

  // --- Private helpers ---

  private handleClientMessage(ws: WebSocket, msg: InboundWsMsg): void {
    switch (msg.type) {
      case 'hello':
        this.sendTo(ws, {
          type: 'hello_ack',
          session_id: TUI_JID,
          assistant_name: ASSISTANT_NAME,
        });
        break;

      case 'message': {
        if (!msg.content?.trim()) return;

        const timestamp = new Date().toISOString();
        const senderName = msg.sender_name || 'User';

        // Register chat metadata
        this.opts.onChatMetadata(TUI_JID, timestamp, 'TUI Chat', 'tui', false);

        const newMsg: NewMessage = {
          id: `tui-${Date.now()}-${crypto.randomBytes(4).toString('hex')}`,
          chat_jid: TUI_JID,
          sender: 'tui-user:local',
          sender_name: senderName,
          content: msg.content,
          timestamp,
          is_from_me: false,
          is_bot_message: false,
        };

        // Track in history
        this.pushHistory({
          content: msg.content,
          sender_name: senderName,
          timestamp,
          is_bot_message: false,
        });

        this.opts.onMessage(TUI_JID, newMsg);
        logger.info(
          { sender: senderName, length: msg.content.length },
          'TUI message received',
        );
        break;
      }

      case 'history_request': {
        const limit = Math.min(msg.limit || 50, this.maxHistory);
        const messages = this.messageHistory.slice(-limit);
        this.sendTo(ws, { type: 'history', messages });
        break;
      }

      default:
        this.sendTo(ws, {
          type: 'error',
          message: `Unknown message type: ${(msg as { type: string }).type}`,
        });
    }
  }

  private sendTo(ws: WebSocket, msg: OutboundWsMsg): void {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
    }
  }

  private broadcast(msg: OutboundWsMsg): void {
    const data = JSON.stringify(msg);
    for (const ws of this.clients) {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data);
      }
    }
  }

  private pushHistory(entry: {
    content: string;
    sender_name: string;
    timestamp: string;
    is_bot_message: boolean;
  }): void {
    this.messageHistory.push(entry);
    if (this.messageHistory.length > this.maxHistory) {
      this.messageHistory.shift();
    }
  }

  private async flushOutgoingQueue(): Promise<void> {
    if (this.flushing || this.outgoingQueue.length === 0) return;
    this.flushing = true;
    try {
      logger.info(
        { count: this.outgoingQueue.length },
        'Flushing TUI outgoing queue',
      );
      while (this.outgoingQueue.length > 0 && this.clients.size > 0) {
        const item = this.outgoingQueue.shift()!;
        const msg: OutboundChatMsg = {
          type: 'message',
          content: item.text,
          sender_name: ASSISTANT_NAME,
          timestamp: new Date().toISOString(),
        };
        this.broadcast(msg);
      }
    } finally {
      this.flushing = false;
    }
  }
}

registerChannel('tui', (opts: ChannelOpts) => new TuiChannel(opts));
