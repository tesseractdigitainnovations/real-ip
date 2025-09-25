# Traefik Real IP Plugin

A comprehensive Traefik plugin that accurately determines the real client IP address when Traefik is deployed behind load balancers, CDNs, or proxy chains.

## Features

- **Multi-Header Support**: Checks multiple headers in configurable order
- **CloudFlare Integration**: Native support for CloudFlare headers
- **Network Exclusion**: Configurable trusted proxy networks
- **IPv6 Support**: Full IPv4 and IPv6 compatibility
- **Chain Protection**: Prevents infinite loops in forwarded-for chains
- **Debug Mode**: Optional logging for troubleshooting
- **Custom Headers**: Set real IP to custom headers

## Installation

### 1. Plugin Configuration (.traefik.yml)

```yaml
pilot:
  token: "your-pilot-token"

experimental:
  plugins:
    traefik-real-ip:
      modulename: "github.com/tesseractdigitainnovations/real-ip"
      version: "v1.0.0"
```

### 2. Dynamic Configuration

#### Docker Compose Labels

```yaml
version: '3.8'
services:
  web:
    image: nginx
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.web.rule=Host(`example.com`)"
      - "traefik.http.routers.web.middlewares=real-ip"
      - "traefik.http.middlewares.real-ip.plugin.traefik-real-ip.excludednets=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
      - "traefik.http.middlewares.real-ip.plugin.traefik-real-ip.trustcloudflare=true"
      - "traefik.http.middlewares.real-ip.plugin.traefik-real-ip.debug=false"
```

#### Kubernetes IngressRoute

```yaml
apiVersion: traefik.containo.us/v1alpha1
kind: Middleware
metadata:
  name: real-ip
spec:
  plugin:
    traefik-real-ip:
      excludednets:
        - "10.0.0.0/8"
        - "172.16.0.0/12"
        - "192.168.0.0/16"
        - "127.0.0.0/8"
      headerorder:
        - "Cf-Connecting-Ip"
        - "True-Client-Ip"
        - "X-Forwarded-For"
      trustcloudflare: true
      overwritexrealip: true
      debug: false
---
apiVersion: traefik.containo.us/v1alpha1
kind: IngressRoute
metadata:
  name: web-route
spec:
  entryPoints:
    - web
  routes:
    - match: Host(`example.com`)
      kind: Rule
      services:
        - name: web-service
          port: 80
      middlewares:
        - name: real-ip
```

#### File Configuration (dynamic.yml)

```yaml
http:
  middlewares:
    real-ip:
      plugin:
        traefik-real-ip:
          excludednets:
            - "10.0.0.0/8"
            - "172.16.0.0/12" 
            - "192.168.0.0/16"
            - "127.0.0.0/8"
          headerorder:
            - "Cf-Connecting-Ip"
            - "True-Client-Ip"
            - "X-Client-Ip"
            - "X-Forwarded-For"
          trustcloudflare: true
          overwritexrealip: true
          addforwardedproto: true
          customheader: "X-Client-Real-IP"
          debug: false
          maxiterations: 10

  routers:
    web:
      rule: "Host(`example.com`)"
      service: "web-service"
      middlewares:
        - "real-ip"

  services:
    web-service:
      loadBalancer:
        servers:
          - url: "http://backend:8080"
```

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `excludednets` | []string | Private networks | CIDR networks to exclude (trusted proxies) |
| `headerorder` | []string | CF, True-Client, X-Client, X-Forwarded-For | Order of headers to check |
| `trustcloudflare` | bool | true | Trust CloudFlare Cf-Connecting-Ip header |
| `overwritexrealip` | bool | true | Overwrite existing X-Real-IP header |
| `addforwardedproto` | bool | false | Add X-Forwarded-Proto header |
| `customheader` | string | "" | Custom header name to set real IP |
| `debug` | bool | false | Enable debug logging |
| `maxiterations` | int | 10 | Max iterations for forwarded-for parsing |

## Default Excluded Networks

```
10.0.0.0/8      - Private network
172.16.0.0/12   - Private network  
192.168.0.0/16  - Private network
127.0.0.0/8     - Loopback
::1/128         - IPv6 loopback
fc00::/7        - IPv6 private
```

## Header Priority Order

1. **Cf-Connecting-Ip** - CloudFlare real client IP
2. **True-Client-Ip** - Akamai and other CDNs
3. **X-Client-Ip** - Generic client IP
4. **X-Cluster-Client-Ip** - Cluster environments
5. **X-Forwarded-For** - Standard proxy header
6. **X-Original-Forwarded-For** - Backup forwarded header

## Usage Examples

### CloudFlare Setup

```yaml
http:
  middlewares:
    cloudflare-realip:
      plugin:
        traefik-real-ip:
          excludednets:
            - "173.245.48.0/20"
            - "103.21.244.0/22"
            - "103.22.200.0/22"
            # Add all CloudFlare IP ranges
          trustcloudflare: true
          headerorder:
            - "Cf-Connecting-Ip"
            - "X-Forwarded-For"
```

### AWS ALB Setup

```yaml
http:
  middlewares:
    alb-realip:
      plugin:
        traefik-real-ip:
          excludednets:
            - "10.0.0.0/8"      # VPC CIDR
          headerorder:
            - "X-Forwarded-For"
          addforwardedproto: true
```

### Development/Debug Setup

```yaml
http:
  middlewares:
    debug-realip:
      plugin:
        traefik-real-ip:
          debug: true
          customheader: "X-Debug-Real-IP"
          overwritexrealip: false
```

## Testing

You can test the plugin using curl:

```bash
# Test basic functionality
curl -H "X-Forwarded-For: 1.2.3.4, 10.0.0.1" http://your-domain.com

# Test CloudFlare header
curl -H "Cf-Connecting-Ip: 1.2.3.4" http://your-domain.com

# Test with multiple headers
curl -H "X-Forwarded-For: 192.168.1.1" \
     -H "True-Client-Ip: 1.2.3.4" \
     http://your-domain.com
```

## Common Scenarios

### Behind AWS Application Load Balancer
```yaml
excludednets:
  - "10.0.0.0/8"  # Your VPC CIDR
headerorder:
  - "X-Forwarded-For"
```

### Behind Google Cloud Load Balancer
```yaml
excludednets:
  - "10.0.0.0/8"
  - "35.191.0.0/16"  # GCP health checks
headerorder:
  - "X-Forwarded-For"
```

### Behind Nginx Proxy
```yaml
excludednets:
  - "192.168.1.0/24"  # Nginx server network
headerorder:
  - "X-Real-IP"
  - "X-Forwarded-For"
```

### Development with Docker
```yaml
excludednets:
  - "172.16.0.0/12"  # Docker networks
  - "192.168.0.0/16"
debug: true
```

## Troubleshooting

1. **Enable Debug Mode**: Set `debug: true` to see IP detection process
2. **Check Headers**: Use browser dev tools to verify headers
3. **Network Configuration**: Ensure excluded networks match your infrastructure
4. **Header Order**: Adjust header priority based on your setup

## Security Considerations

- Only trust headers from known proxy networks
- Regularly update excluded networks list
- Monitor for header spoofing attempts
- Use HTTPS to prevent header manipulation
