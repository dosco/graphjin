---
id: deploy
title: How to deploy GraphJin
sidebar_label: Deploy
---

Since you're reading this you're probably considering deploying GraphJin. You're in luck it's really easy and there are several ways to choose from. Keep in mind GraphJin can be used as a pre-built docker image or you can easily customize it and build your own docker image.

:::info JWT tokens (Auth0, etc)
When deploying on a subdomain and configure this service to use JWT authentication. You will need the public key file or secret key. Ensure your web app passes the JWT token with every GraphQL request (Cookie recommended). You have to add the web domain to the `cors_allowed_origins` config option so CORS can allow the browser to do cross-domain ajax requests.
:::

## Google Cloud Run (Fully Managed)

Cloud Run is a fully managed compute platform for deploying and scaling containerized applications quickly and securely.
Your GraphJin app comes with a `cloudbuild.yaml` file so it's really easy to use Google Cloud Build to build and deploy your GraphJin app to Google Cloud Run.

:::note
Remember to give Cloud Build permission to deploy to Cloud Run first this can be done in the Cloud Build settings screen. Also the service account you use with Cloud Run must have the IAM permissions to connect to CloudSQL. https://cloud.google.com/sql/docs/postgres/connect-run
:::

Use the command below to tell Cloud Build to build and deploy your app.

```bash
gcloud build submit --substitutions=SERVICE_ACCOUNT=admin@my-project.iam.gserviceaccount.com,REGION=us-central1 .
```

:::info Secrets Management
Your secrets like the database password should be managed by the Mozilla SOPS app. This is a secrets management app that encrypts all your secrets and stores them in a file to be decrypted in production using the Cloud KMS (Google Cloud KMS Or Amazon KMS). Our cloud build file above expects the secrets file to be `config/prod.secrets.yml`. You can find more information on Mozilla SOPS on their site. https://github.com/mozilla/sops
:::

## Build Docker Image Locally

If for whatever reason you decide to build your own Docker images then just use the command below.

```bash
docker build -t your-api-service-name .
```

## With a Rails app

GraphJin can read Rails session cookies, like those created by authentication gems (Devise or Warden). Based on how you've configured your Rails app the cookie can be signed, encrypted, both, include the user ID or just have the ID of the session. If you have choosen to use Redis or Memcache as your session store then GraphJin can read the session cookie and then lookup the user in the session store. In short it works really well with almost all Rails apps.

For any of this to work GraphJin must be deployed in a way that make the browser send the apps cookie to it along with the GraphQL query. That means GraphJin needs to be either on the same domain as your app or on a subdomain.

:::info I need an example
Say your Rails app runs on `myrailsapp.com` then GraphJin should be on the same domain or on a subdomain like `graphql.myrailsapp.com`. If you choose subdomain then remeber read the [Deploy under a subdomain](#deploy-under-a-subdomain) section.
:::

## Deploy under a subdomain

For this to work you have to ensure that the option `:domain => :all` is added to your Rails app config `Application.config.session_store` this will cause your rails app to create session cookies that can be shared with sub-domains. More info here [/sharing-a-devise-user-session-across-subdomains-with-rails](http://excid3.com/blog/sharing-a-devise-user-session-across-subdomains-with-rails-3/)

## With NGINX

If your infrastructure is fronted by NGINX then it should be configured so that all requests to your GraphQL API path are proxyed to GraphJin. In the example NGINX config below all requests to the path `/api/v1/graphql` are routed to wherever you have GraphJin installed within your architecture. This example is derived from the config file example at [/microservices-nginx-gateway/nginx.conf](https://github.com/launchany/microservices-nginx-gateway/blob/master/nginx.conf)

:::info NGINX with sub-domain
Yes, NGINX is very flexible and you can configure it to keep GraphJin a subdomain instead of on the same top level domain. I'm sure a little Googleing will get you some great example configs for that.
:::

```nginx
# Configuration for the server
server {

	# Running port
	listen 80;

	# Proxy the graphql api path to GraphJin
	location /api/v1/graphql {

			proxy_pass         http://graphjin-service:8080;
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

## On Kubernetes

If your Rails app runs on Kubernetes then ensure you have an ingress config deployed that points the path to the service that you have deployed GraphJin under.

### Ingress config

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

### Service and deployment config

```yaml
apiVersion: v1
kind: Service
metadata:
  name: graphql-service
  labels:
    run: graphjin
spec:
  ports:
    - port: 8080
      protocol: TCP
  selector:
    run: graphjin

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: graphjin
spec:
  selector:
    matchLabels:
      run: graphjin
  replicas: 2
  template:
    metadata:
      labels:
        run: graphjin
    spec:
      containers:
        - name: graphjin
          image: docker.io/dosco/graphjin:latest
          ports:
            - containerPort: 8080
```
