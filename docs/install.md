---
sidebar: auto
---

## How to deploy Super Graph

Since you're reading this you're probably considering deploying Super Graph. You're in luck it's really easy and there are several ways to choose from. Keep in mind Super Graph can be used as a pre-built docker image or you can easily customize it and build your own docker image.

### Alongside an existing Rails app

Super Graph can read Rails session cookies. Like those created by authentication gems (Devise or Warden). Based on how you've configured your Rails app the cookie can be signed, encrypted, both, include the user ID or just have the ID of the session. If you have choosen to use Redis or Memcache as your session store then Super Graph can read the session cookie and then lookup the user in the session store. In short it works really well with any kind of Rails app setup.

For any of this to work Super Graph must be deployed in a way that make the browser sent your apps cookie to it along with the GraphQL query. This means Super Graph should be on the same domain as your app or a subdomain.

::: tip I need an example
Say your Rails app runs on `myrailsapp.com` then Super Graph should be on the same domain or on a subdomain like `graphql.myrailsapp.com`. If you choose subdomain read below.
:::

### Custom Docker Image

You might find the need to customize the Super Graph config file to fit your app and then package it into a docker image. This is easy to do below is an example `Dockerfile` to do exactly this. And to build it `docker build -t my-super-graph .`

```docker
FROM dosco/super-graph:latest
WORKDIR /app
COPY *.yml ./
```

### Deploy under a subdomain

For this to work you have to ensure that the option `:domain => :all` is added to your Rails app config `Application.config.session_store` this will cause your rails app to create session cookies that can be shared with sub-domains. More info here [/sharing-a-devise-user-session-across-subdomains-with-rails](http://excid3.com/blog/sharing-a-devise-user-session-across-subdomains-with-rails-3/)

### With an NGINX loadbalancer

If you're infrastructure is fronted by NGINX then it should be configured so that all requests to your GraphQL API path are proxyed to Super Graph. In the example NGINX config below all requests to the path `/api/v1/graphql` are routed to wherever you have Super Graph installed within your architecture. This example is derived from the config file example at [/microservices-nginx-gateway/nginx.conf](https://github.com/launchany/microservices-nginx-gateway/blob/master/nginx.conf)

```nginx
# Configuration for the server
server {

	# Running port
	listen 80;

	# Proxy the graphql api path to Super Graph
	location /api/v1/graphql {

			proxy_pass         http://super-graph-service:8080;
			proxy_redirect     off;
			proxy_set_header   Host $host;
			proxy_set_header   X-Real-IP $remote_addr;
			proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
			proxy_set_header   X-Forwarded-Host $server_name;

	}

	# Proxying all other paths to your Rails app
	location / {

			proxy_pass         http://your-rails-app:3000;
			proxy_redirect     off;
			proxy_set_header   Host $host;
			proxy_set_header   X-Real-IP $remote_addr;
			proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
			proxy_set_header   X-Forwarded-Host $server_name;

	}
}
```

### On Kubernetes

If your Rails app runs on Kubernetes then ensure you have an ingress config deployed that points the path to the service that you have deployed Super Graph under.

#### Ingress config

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: simple-rails-app
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  rules:
  - host: myrailsapp.com
    http:
      paths:
      - path: /api/v1/graphql
        backend:
          serviceName: graphql-service
          servicePort: 8080
      - path: /
        backend:
          serviceName: rails-app
          servicePort: 3000
```

#### Service and deployment config

```yaml
apiVersion: v1
kind: Service
metadata:
  name: graphql-service
  labels:
    run: super-graph
spec:
  ports:
  - port: 8080
    protocol: TCP
  selector:
    run: super-graph

---

apiVersion: apps/v1
kind: Deployment
metadata:
  name: super-graph
spec:
  selector:
    matchLabels:
      run: super-graph
  replicas: 2
  template:
    metadata:
      labels:
        run: super-graph
    spec:
      containers:
      - name: super-graph
        image: docker.io/dosco/super-graph:latest
        ports:
        - containerPort: 8080
```

### JWT tokens (Auth0, etc)

In that case deploy under a subdomain and configure this service to use JWT authentication. You will need the public key file or secret key. Ensure your web app passes the JWT token with every GQL request in the Authorize header as a `bearer` token.


