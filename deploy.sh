#!/bin/bash

set -e

echo "🔁 Pulling latest code from Git..."
git pull origin main

echo "🔨 Building Go binary..."
go build -o swiftfiat

echo "🧹 Reloading systemd config (if changed)..."
sudo systemctl daemon-reload

echo "🚀 Restarting swiftfiat service..."
sudo systemctl restart swiftfiat

echo "📋 Checking service status..."
sudo systemctl status swiftfiat --no-pager

echo "📄 Showing last 10 log lines:"
journalctl -u swiftfiat -n 10 --no-pager

echo "✅ Deployment complete!"
