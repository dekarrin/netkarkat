# netkarkat
Netkarkat (or `netkk` as invoked) is a terminal for sending/receiving binary content, quickly, in interactive
mode.

It was created because sometimes, pure netcat just ain't good enough.

## Installation

First, download one of the releases from the release section. Then, extract it.

```bash
$ tar xzf netkk-latest-amd64-linux.tar.gz
```

### *Nix (Mac, Linux, etc)
Take the executable file `netkk` inside and copy it somewhere on your `$PATH`.

### Windows
For Windows, take the executable file `netkkcmd.exe` and the launch file `netkk.bat` and place them in the same directory somewhere on your path.

## Basic Usage
To open a UDP connection and begin communicating with a remote host:

```
# opens a terminal to send UDP packets to localhost at port 8008
netkk -p udp -r 127.0.0.1:8008
```

To open a UDP connection and wait for the first client to connect and then communicate with that client:

```
# listens on port 8382 for the first UDP client to connect
netkk -p udp -l 127.0.0.1:8382
```

To open a TCP client connection:

```
netkk -p tcp -r 127.0.0.1:8282
```

To open a TCP server connection, use the `-l` flag instead of `-r`. In this
case, the local address in the bind address.

```
netkk -p tcp -l 0.0.0.0:28300
```

When opening a TCP server, the address segement of the `-l` can be dropped to
give only the port, and the bind address will default to 127.0.0.1 in that case:

```
netkk -p tcp -l 28300
```

## TLS/SSL
Netkarkat can handle SSL connections. Currently, only TLS server certificates
over TCP are supported; TLS over UDP ("Datagram TLS" or "DTLS") is not supported
at this time, and TLS for client authentication is also unsupported.

To use SSL connections, give the `--ssl` argument:

```
netkk -p tcp -r mysite.domain:443 --ssl
```

### Dealing with Certificate Validation Errors
If the server responds with certs that aren't trusted by the system,
`--insecure-skip-verify` can be used to disable all host certificate
validation and verification. This should not be used for production systems
as it bypasses components of the TLS protocol that ensure security.

```
netkk -p tcp -r mysite.domain:443 --ssl --insecure-skip-verify
```

A better way would be to obtain the signer chain for the certificate authority
who signed the server's certificate and tell netkarkat to include it in its
trusted chains with the `--trustchain` argument.

```
netkk -p tcp -r mysite.domain:443 --ssl --trustchain path/to/ca/signerchain.pem
```

### Server Certificate Use
If running netkarkat in TCP server mode with SSL-enabled, the certificate and
keyfile to use can be specified by using the `--server-cert-file` and
`--server-key-file` flags:

```
netkk -p tcp -l 8335 --ssl --server-cert-file somecert.pem --server-key-file somecert.key.pem
```

If a certificate is not provided using those two arguments, netkarkat will
automatically generate a self-signed certificate using a generated certificate
authority:

```
netkk -p tcp -l 8335 --ssl
```

The CA will be generated on-the-fly and will be written to disk at
startup prior to the creation of the certificate; this allows other programs who
would communicate with netkarkat to put that CA in their pool of trusted CAs.

Failure to specify the CA in the client program's trust pool is likely to make
the client program fail to validate the generated certificate.

If a generated cert is used, some aspects can be changed. The common name can be
specified with `--cert-common-name`, and the IP addresses can be set with
`--cert-ips`.

```
netkk -l 8335 --ssl --cert-common-name netkkHost.local.network

netkk -l 8335 --ssl --cert-ips 127.0.0.1,10.140.12.233
```
