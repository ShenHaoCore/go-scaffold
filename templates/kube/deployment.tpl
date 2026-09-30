{{/*
  警告：goctl 默认 kube 模板，非本仓生产真源。
  - 下方 tcpSocket 仅验端口开闭，不等于 HTTP /health 或 gRPC Health 语义。
  - Probe 未设 timeoutSeconds；生产须 ≥ HealthCheckTimeout（config/*.yaml）。
  - HPA 已用 autoscaling/v2（勿再用 v2beta2）。
  生产清单：deploy/deployment.yaml（api=httpGet /health*；rpc=exec grpc_health_probe）。
  见 templates/README.md
*/}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    app: {{.Name}}
spec:
  replicas: {{.Replicas}}
  revisionHistoryLimit: {{.Revisions}}
  selector:
    matchLabels:
      app: {{.Name}}
  template:
    metadata:
      labels:
        app: {{.Name}}
    spec:{{if .ServiceAccount}}
      serviceAccountName: {{.ServiceAccount}}{{end}}
      containers:
      - name: {{.Name}}
        image: {{.Image}}
        {{if .ImagePullPolicy}}imagePullPolicy: {{.ImagePullPolicy}}
        {{end}}ports:
        - containerPort: {{.Port}}
        # 参考件：tcpSocket ≠ readiness；生产见 deploy/deployment.yaml
        readinessProbe:
          tcpSocket:
            port: {{.Port}}
          initialDelaySeconds: 5
          periodSeconds: 10
          # timeoutSeconds: 建议 ≥ HealthCheckTimeout（config/*.yaml）
        livenessProbe:
          tcpSocket:
            port: {{.Port}}
          initialDelaySeconds: 15
          periodSeconds: 20
          # timeoutSeconds: 建议 ≥ HealthCheckTimeout（config/*.yaml）
        resources:
          requests:
            cpu: {{.RequestCpu}}m
            memory: {{.RequestMem}}Mi
          limits:
            cpu: {{.LimitCpu}}m
            memory: {{.LimitMem}}Mi
        volumeMounts:
        - name: timezone
          mountPath: /etc/localtime
      {{if .Secret}}imagePullSecrets:
      - name: {{.Secret}}
      {{end}}volumes:
        - name: timezone
          hostPath:
            path: /usr/share/zoneinfo/Asia/Shanghai

---

apiVersion: v1
kind: Service
metadata:
  name: {{.Name}}-svc
  namespace: {{.Namespace}}
spec:
  ports:
  {{if .UseNodePort}}- nodePort: {{.NodePort}}
    port: {{.Port}}
    protocol: TCP
    targetPort: {{.TargetPort}}
  type: NodePort{{else}}- port: {{.Port}}
    targetPort: {{.TargetPort}}{{end}}
  selector:
    app: {{.Name}}

---

apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{.Name}}-hpa-c
  namespace: {{.Namespace}}
  labels:
    app: {{.Name}}-hpa-c
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{.Name}}
  minReplicas: {{.MinReplicas}}
  maxReplicas: {{.MaxReplicas}}
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 80

---

apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{.Name}}-hpa-m
  namespace: {{.Namespace}}
  labels:
    app: {{.Name}}-hpa-m
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{.Name}}
  minReplicas: {{.MinReplicas}}
  maxReplicas: {{.MaxReplicas}}
  metrics:
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
