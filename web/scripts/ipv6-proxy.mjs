import net from "node:net";

const port = 4317;
const server = net.createServer((socket) => {
  const upstream = net.connect(port, "127.0.0.1");
  socket.pipe(upstream);
  upstream.pipe(socket);
  upstream.on("error", () => socket.destroy());
  socket.on("error", () => upstream.destroy());
});

server.on("error", (error) => {
  if (error && error.code === "EADDRINUSE") {
    console.log("IPv6 proxy is already listening on ::4317");
    process.exit(0);
  }
  console.error(error);
  process.exit(1);
});

server.listen({ port, host: "::", ipv6Only: true }, () => {
  console.log("IPv6 proxy [::]:4317 -> 127.0.0.1:4317");
});
