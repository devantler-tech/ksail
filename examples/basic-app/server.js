const http = require('http');
const os = require('os');

const VERSION = process.env.VERSION || '1.0.0';
const PORT = process.env.PORT || 3000;
const HOSTNAME = os.hostname();

const server = http.createServer((req, res) => {
  const response = {
    message: 'Hello from KSail!',
    version: VERSION,
    hostname: HOSTNAME,
    timestamp: new Date().toISOString()
  };

  res.writeHead(200, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(response, null, 2));

  console.log(`[${new Date().toISOString()}] ${req.method} ${req.url} - ${res.statusCode}`);
});

server.listen(PORT, () => {
  console.log(`Server running on port ${PORT}`);
  console.log(`Version: ${VERSION}`);
  console.log(`Hostname: ${HOSTNAME}`);
});

// Graceful shutdown
process.on('SIGTERM', () => {
  console.log('SIGTERM received, shutting down gracefully');
  server.close(() => {
    console.log('Server closed');
    process.exit(0);
  });
});
