> [!WARNING]
> This is not tested in production environments; **Use it at your own risk.**

# superderper

There's [derper](https://tailscale.com/kb/1232/derp-servers), and then there's superderper.
This is my attempt to make a derper server serve multiple tailnets with verification.
This idea has been around for a [long](https://github.com/tailscale/tailscale/issues/4197) [time](https://github.com/tailscale/tailscale/issues/10359),
but seem to have low interest.

derper provides `-verify-clients` to verify clients based on a Tailscaled instance.
A downside of this approach is that it only supports a single tailnet, as tailscaled
instances can only be logged into one at a time.
You can use containers to run multiple tailscaled and derper instances and reverse-proxy them,
but I'd like to avoid that.

Instead, we run a single derper instance with multiple tailscaled instances with different control sockets.
By using derper's `-verify-clients-url`, we can create an HTTP server that checks if the request matches an instance.
This means you don't even need configure reverse proxies, etc.
It should just work as you add more tailscaled instances.
This repo contains a bunch of files that helps you achieve this setup.

![How it works](https://github.com/takase1121/superderper/blob/master/diagram.png)

## Instructions

> [!NOTE]
> This is only tested on Arch Linux.

Build the superderper program. This is the HTTP server that communicates with tailscaled instances and derper.

```
$ go build
```

Copy the files to appropriate places.

```
# cp superderper.conf /etc/default/superderper
# cp tailscaled-derper.conf /etc/default/tailscaled-derper
# cp superderper.service tailscaled-derper@.service /usr/lib/systemd/system
```

Set up tailscaled instances for superderper.

```
# systemctl start tailscaled-derper@first.service
# systemctl start tailscaled-derper@second.service

# tailscale --socket /run/superderper/tailscaled-first.sock login
# tailscale --socket /run/superderper/tailscaled-second.sock login
```

Start superderper.

```
# systemctl start superderper
```

Configure derper to use superderper to verify clients.

```
derper -a :9600 -hostname=example.com -verify-client-url=http://127.0.0.1:15300/validate -verify-client-url-fail-open=false
```
