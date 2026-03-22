# MailMate

WhatsApp-powered assistant that integrates with Gmail, Outlook, Google Calendar, and Microsoft Calendar. Get email notifications, manage meetings, and track personal goals — all from WhatsApp.

## Features

- **Email Integration** — Connect Gmail and Outlook via OAuth2. View recent emails and unread counts.
- **Calendar Management** — View, create, reschedule, and cancel meetings on Google Calendar and Microsoft Calendar.
- **Natural Language Chatbot** — Talk naturally on WhatsApp. Powered by OpenAI for intent recognition and entity extraction.
- **Meeting Reminders** — Automatic WhatsApp notifications 15 minutes before meetings.
- **Daily Summaries** — Morning digest of unread emails, today's meetings, and tracking goals.
- **Goal & Habit Tracking** — Track gym progress, habits, or any goal over a custom period with daily reminders and progress bars.
- **Encrypted Token Storage** — OAuth tokens encrypted at rest with AES-256-GCM.

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────────┐
│  WhatsApp    │────▶│  Twilio API  │────▶│  MailMate API   │
│  (User)      │◀────│  (Webhook)   │◀────│  (Go/Gin)       │
└─────────────┘     └──────────────┘     └────────┬────────┘
                                                   │
                    ┌──────────────────────────────┼──────────────────────┐
                    │                              │                      │
              ┌─────▼─────┐  ┌─────────────┐  ┌───▼──────┐  ┌──────────┐
              │ Google API │  │ MS Graph API│  │PostgreSQL│  │  Redis   │
              │ Gmail+Cal  │  │ Outlook+Cal │  │ (Data)   │  │ (Cache)  │
              └───────────┘  └─────────────┘  └──────────┘  └──────────┘
                                                   │
                                           ┌───────▼───────┐
                                           │  OpenAI API   │
                                           │  (NLP/Intent) │
                                           └───────────────┘
```

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.22 |
| HTTP Framework | Gin |
| Database | PostgreSQL 16 |
| Cache | Redis 7 |
| WhatsApp | Twilio WhatsApp Business API |
| Email (Google) | Gmail API + Google Calendar API |
| Email (Microsoft) | Microsoft Graph API |
| NLP | OpenAI GPT-4o (function calling) |
| Auth | OAuth 2.0 (PKCE) |
| Encryption | AES-256-GCM |
| Container | Docker + Docker Compose |
| Orchestration | Kubernetes + Helm |
| Logging | Uber Zap (structured JSON) |
| Scheduling | robfig/cron |

## Project Structure

```
mailmate/
├── cmd/server/              # Application entrypoint
│   └── main.go
├── internal/
│   ├── config/              # Environment-based configuration
│   ├── domain/              # Domain models and interfaces
│   ├── store/               # PostgreSQL repository + encryption
│   ├── provider/            # External API integrations
│   │   ├── google.go        # Gmail + Google Calendar
│   │   ├── microsoft.go     # Outlook + MS Calendar (Graph API)
│   │   ├── whatsapp.go      # Twilio WhatsApp sender
│   │   └── openai.go        # NLP intent parser
│   ├── service/             # Business logic
│   │   ├── auth.go          # OAuth flow management
│   │   ├── chatbot.go       # Message routing + intent handling
│   │   ├── notification.go  # Scheduled reminders & summaries
│   │   └── tracker.go       # Goal/habit tracking
│   ├── handler/             # HTTP handlers
│   │   ├── webhook.go       # Twilio WhatsApp webhook
│   │   ├── oauth.go         # OAuth callbacks
│   │   └── health.go        # Health checks
│   ├── middleware/           # HTTP middleware
│   └── server/              # Server wiring & startup
├── migrations/              # SQL migrations
├── deploy/helm/mailmate/    # Helm chart for Kubernetes
├── Dockerfile
├── docker-compose.yaml
├── Makefile
└── .env.example
```

## Prerequisites

- Go 1.22+
- PostgreSQL 16+
- Redis 7+
- [Twilio Account](https://www.twilio.com/) with WhatsApp Sandbox or Business API
- [Google Cloud Console](https://console.cloud.google.com/) project with Gmail & Calendar APIs enabled
- [Azure App Registration](https://portal.azure.com/) for Microsoft Graph
- [OpenAI API Key](https://platform.openai.com/)

## Quick Start

### 1. Clone & Configure

```bash
git clone https://github.com/hemantkpr/mailmate.git
cd mailmate
cp .env.example .env
# Edit .env with your credentials
```

### 2. Generate Encryption Key

```bash
openssl rand -hex 32
# Paste the output into ENCRYPTION_KEY in .env
```

### 3. Start with Docker Compose

```bash
make docker-up
```

This starts PostgreSQL, Redis, and the MailMate server. Migrations run automatically.

### 4. Configure Twilio Webhook

In your Twilio Console, set the WhatsApp webhook URL to:
```
https://your-domain.com/webhook/whatsapp
```

### 5. Start Chatting

Send a message to your Twilio WhatsApp number. MailMate will guide you through connecting your email accounts.

## Development

```bash
# Install dependencies
go mod download

# Run locally (requires Postgres & Redis)
make dev

# Run tests
make test

# Run linter
make lint

# Build binary
make build
```

## API Configuration

### Google OAuth Setup

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a project and enable Gmail API + Google Calendar API
3. Create OAuth 2.0 credentials (Web application)
4. Add redirect URI: `https://your-domain.com/oauth/google/callback`
5. Copy Client ID and Secret to `.env`

### Microsoft OAuth Setup

1. Go to [Azure Portal](https://portal.azure.com/) → App Registrations
2. Register a new application
3. Add redirect URI: `https://your-domain.com/oauth/microsoft/callback`
4. Under API Permissions, add: `Mail.Read`, `Calendars.ReadWrite`, `offline_access`
5. Create a client secret
6. Copy Application ID, Secret, and Tenant ID to `.env`

### Twilio WhatsApp Setup

1. Sign up at [Twilio](https://www.twilio.com/)
2. Activate WhatsApp Sandbox (for testing) or apply for WhatsApp Business API
3. Configure webhook URL: `https://your-domain.com/webhook/whatsapp` (POST)
4. Copy Account SID, Auth Token, and WhatsApp number to `.env`

## Deployment (Kubernetes)

```bash
# Install with Helm
helm install mailmate deploy/helm/mailmate -f deploy/helm/mailmate/values.yaml

# Upgrade
helm upgrade mailmate deploy/helm/mailmate -f deploy/helm/mailmate/values.yaml
```

For production, use sealed-secrets or external-secrets-operator for managing secrets instead of plain values.

## WhatsApp Commands

| Command | Example |
|---------|---------|
| Connect email | "Connect my email" |
| View emails | "Show my emails" |
| View meetings | "What meetings do I have today?" |
| Reschedule | "Spoke to John, he's busy — move meeting to tomorrow same time" |
| Create meeting | "Schedule a team sync at 2pm Friday" |
| Cancel meeting | "Cancel my meeting with Sarah" |
| Start tracking | "Track my gym progress for 90 days" |
| Log progress | "Did gym today — chest and arms" |
| View progress | "Show my tracking progress" |
| Daily summary | "Give me my daily summary" |
| Help | "Help" |

## Database

**PostgreSQL** is used for persistent storage:
- User accounts and preferences
- Encrypted OAuth tokens (AES-256-GCM)
- Tracking goals and daily entries
- Conversation history (for context-aware chatbot)

**Redis** is used for:
- OAuth state parameter storage (10-minute TTL)
- Session caching

## Security

- OAuth tokens encrypted at rest (AES-256-GCM)
- Twilio webhook signature validation (HMAC-SHA1)
- Security headers (HSTS, CSP, X-Frame-Options, etc.)
- Rate limiting on all endpoints
- Non-root container user
- Secrets managed via Kubernetes Secrets

## Command
ngrok http 8080

## License

MIT
