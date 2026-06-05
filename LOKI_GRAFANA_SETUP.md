# Loki + Grafana Logging Setup

This project now uses **Loki** for centralized log aggregation and **Grafana** for visualization, replacing Papertrail.

## Architecture

- **Loki**: A log aggregation system that stores logs in a time-series database
- **Grafana**: A visualization platform for querying and analyzing logs
- **API**: Sends logs to Loki via HTTP POST requests

## Getting Started

### 1. Environment Configuration

Update your `.env` file with the Loki URL:

```env
LOKI_URL=http://loki:3100/loki/api/v1/push
```

For local development without Docker:
```env
LOKI_URL=http://localhost:3100/loki/api/v1/push
```

### 2. Start Services with Docker

```bash
docker-compose up -d
```

This will start:
- **API** on `http://localhost:9000`
- **Loki** on `http://localhost:3100`
- **Grafana** on `http://localhost:3000`
- **Redis** on `http://localhost:6379`

### 3. Access Grafana

1. Open `http://localhost:3000` in your browser
2. Login with:
   - Username: `admin`
   - Password: `admin`

### 4. Add Loki as Data Source

1. In Grafana, go to **Connections** → **Data sources**
2. Click **+ Add data source**
3. Select **Loki**
4. Set the URL to `http://loki:3100`
5. Click **Save & Test**

### 5. Create Dashboards

1. Go to **Dashboards** → **Create** → **New dashboard**
2. Add a panel and query logs using LogQL:

#### Common LogQL Queries

**All logs from the API:**
```logql
{job="swiftfiat-api"}
```

**Logs by environment:**
```logql
{job="swiftfiat-api", env="release"}
```

**Logs by level:**
```logql
{job="swiftfiat-api", level="error"}
```

**Search for specific text:**
```logql
{job="swiftfiat-api"} |= "error"
```

**Count logs over time:**
```logql
rate({job="swiftfiat-api"}[1m])
```

## Log Structure

Each log entry includes:
- **timestamp**: When the log was created
- **level**: Log level (debug, info, warn, error)
- **message**: The main log message
- **fields**: Additional structured data (method, path, status, duration, etc.)

### Labels

Logs are tagged with the following labels for filtering:
- `job`: `swiftfiat-api`
- `env`: Environment (from config, e.g., "release", "dev")
- `level`: Log level (debug, info, warning, error, fatal, panic)

## Configuration Files

- **`loki-config.yaml`**: Loki configuration for storage and retention
- **`docker-compose.yml`**: Defines all services and their configurations

## Troubleshooting

### Loki not receiving logs

1. Check that Loki is running:
   ```bash
   docker logs swiftfiat_loki
   ```

2. Verify the API can reach Loki:
   ```bash
   docker exec swiftfiat_api curl -X GET http://loki:3100/ready
   ```

3. Check the LOKI_URL environment variable is set correctly

### Grafana not showing logs

1. Verify Loki data source is properly configured
2. Check the query syntax is correct
3. Make sure logs have been generated (the query time range should match)

## Performance Considerations

- Logs are sent asynchronously to avoid blocking requests
- Large payloads (>250 bytes) are not logged in requests to avoid storage bloat
- HTTP requests to Loki have a 5-second timeout
- WebSocket logs are captured separately

## Retention Policy

By default, Loki retains logs indefinitely. To modify retention, edit `loki-config.yaml`:

```yaml
table_manager:
  retention_deletes_enabled: true
  retention_period: 720h  # 30 days
```

## Migration from Papertrail

All previous Papertrail configuration (`PAPERTRAIL`, `PAPERTRAIL_APP_NAME`) has been removed and replaced with `LOKI_URL`.

### What Changed

| Before | After |
|--------|-------|
| Syslog Hook | HTTP POST Hook |
| Remote syslog endpoint | Loki API endpoint |
| Papertrail web UI | Grafana dashboards |
| Fixed retention | Configurable retention |

## Next Steps

1. Set up alerts in Grafana for critical errors
2. Create custom dashboards for monitoring
3. Integrate with PagerDuty or other alerting services
4. Configure backup of Loki data for disaster recovery
