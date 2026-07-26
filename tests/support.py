"""Shared test helpers."""

from __future__ import annotations

import socketserver
from http.server import BaseHTTPRequestHandler, HTTPServer


class LocalHTTPServer(HTTPServer):
    """HTTPServer that binds without a reverse DNS lookup.

    http.server's server_bind() resolves the bind address through
    socket.getfqdn(). On a host with no reverse resolver that call blocks until
    it times out -- roughly 35s on the macOS CI runners -- and it only fills in
    self.server_name, which no test reads. oneagent.server carries the same
    override for the product; mock servers need it so the suite is not paying
    the stall twice per job.
    """

    def server_bind(self) -> None:
        socketserver.TCPServer.server_bind(self)
        host, port = self.server_address[:2]
        self.server_name = host
        self.server_port = port


def start_local_server(handler: type[BaseHTTPRequestHandler]) -> LocalHTTPServer:
    return LocalHTTPServer(("127.0.0.1", 0), handler)
