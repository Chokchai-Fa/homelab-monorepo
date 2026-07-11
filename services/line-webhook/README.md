# LINE Messaging API Webhook with Go and Echo

A webhook server for LINE Messaging API built with Go and Echo framework. This server can receive and respond to LINE messages, handle follow/unfollow events, and process postback events.

## Features

- ✅ Webhook endpoint for LINE Messaging API
- ✅ Message echo functionality
- ✅ Signature verification for security
- ✅ Follow/Unfollow event handling
- ✅ Postback event processing
- ✅ Health check endpoint
- ✅ CORS support
- ✅ Structured logging

## Prerequisites

- Go 1.23 or later
- LINE Developer Account
- LINE Bot Channel (Messaging API)

## Quick Start

### 1. Clone and Setup

```bash
cd /path/to/your/workspace
# If you haven't cloned yet, the files are already here
```

### 2. Configure Environment

Copy the example environment file and update with your LINE Bot credentials:

```bash
cp .env.example .env
```

Edit `.env` file with your LINE Bot credentials:

```env
LINE_CHANNEL_SECRET=your_actual_channel_secret
LINE_CHANNEL_ACCESS_TOKEN=your_actual_access_token
PORT=8080
```

### 3. Install Dependencies

```bash
go mod tidy
```

### 4. Run the Server

```bash
go run main.go
```

The server will start on `http://localhost:8080`

## Getting LINE Bot Credentials

### Step 1: Create LINE Developer Account

1. Go to [LINE Developers Console](https://developers.line.biz/)
2. Login with your LINE account
3. Create a new provider or use existing one

### Step 2: Create Messaging API Channel

1. Click "Create a new channel"
2. Select "Messaging API"
3. Fill in the required information:
   - Channel name: Your bot name
   - Channel description: Description of your bot
   - Category: Select appropriate category
   - Subcategory: Select appropriate subcategory
4. Click "Create"

### Step 3: Get Credentials

1. Go to your channel's "Basic settings" tab
2. Copy the **Channel Secret**
3. Go to "Messaging API" tab
4. Copy the **Channel Access Token** (generate if not available)

### Step 4: Configure Webhook URL

1. In the "Messaging API" tab, find "Webhook settings"
2. Set webhook URL to: `https://your-domain.com/webhook`
3. Enable "Use webhook"
4. Verify the webhook URL (make sure your server is running and accessible)

## API Endpoints

### Health Check
```
GET /
```
Returns server status.

**Response:**
```json
{
  "status": "ok",
  "message": "LINE Bot Webhook Server is running"
}
```

### Webhook Endpoint
```
POST /webhook
```
Receives LINE events and processes them.

**Headers:**
- `X-Line-Signature`: Required signature for verification

## Supported Events

### Text Messages
- Echoes back the received message
- Special commands:
  - `hello`, `hi` → Greeting response
  - `help` → Shows available commands

### Follow Events
- Welcomes new users who add the bot as a friend

### Unfollow Events
- Logs when users remove the bot

### Postback Events
- Handles postback data from interactive messages

## Development

### Project Structure

```
line-webhook/
├── main.go           # Main application
├── go.mod           # Go module file
├── go.sum           # Go dependencies
├── .env.example     # Environment template
├── .env             # Your environment (create from .env.example)
├── .gitignore       # Git ignore rules
└── README.md        # This file
```

### Building for Production

```bash
# Build binary
go build -o line-webhook main.go

# Run binary
./line-webhook
```

### Docker Deployment (Optional)

```bash
# Build image
docker build -t line-webhook .

# Run container
docker run -p 8080:8080 \
  -e LINE_CHANNEL_SECRET=your_secret \
  -e LINE_CHANNEL_ACCESS_TOKEN=your_token \
  line-webhook
```

## Deployment Options

### 1. Railway
1. Connect your GitHub repository
2. Set environment variables in Railway dashboard
3. Deploy automatically

### 2. Heroku
1. Create new Heroku app
2. Set config vars (environment variables)
3. Deploy via Git or GitHub integration

### 3. Google Cloud Run
1. Build and push Docker image to Google Container Registry
2. Deploy to Cloud Run
3. Set environment variables

### 4. AWS Lambda (with adapter)
You'll need to modify the code to work with AWS Lambda using a framework like `aws-lambda-go`.

## Security Notes

- ✅ Signature verification is implemented
- ✅ Environment variables for sensitive data
- ✅ CORS headers configured
- ✅ Request body validation

## Troubleshooting

### Common Issues

1. **Signature verification fails**
   - Check your Channel Secret is correct
   - Ensure the webhook URL is using HTTPS in production

2. **Bot doesn't respond**
   - Verify Channel Access Token
   - Check server logs for errors
   - Ensure webhook URL is accessible from LINE servers

3. **Server won't start**
   - Check if port is available
   - Verify Go version (1.23+ required)
   - Check environment variables are set

### Logs

The server logs all events and errors. Check the console output for debugging information.

## License

MIT License - feel free to use this code for your projects!

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## Support

For LINE Bot development questions:
- [LINE Developers Documentation](https://developers.line.biz/en/docs/)
- [LINE Bot SDK for Go](https://github.com/line/line-bot-sdk-go)

For Echo framework questions:
- [Echo Documentation](https://echo.labstack.com/)