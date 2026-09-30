{{/*
  警告：goctl 默认 kube 模板参考件，非本仓生产真源。
  生产清单：deploy/deployment.yaml。见 templates/README.md
*/}}
apiVersion: batch/v1
kind: CronJob
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  successfulJobsHistoryLimit: {{.SuccessfulJobsHistoryLimit}}
  schedule: "{{.Schedule}}"
  jobTemplate:
    spec:
      template:
        spec:
{{if .ServiceAccount}}
          serviceAccountName: {{.ServiceAccount}}
{{end}}
          restartPolicy: OnFailure
          containers:
          - name: {{.Name}}
            image: {{.Image}}
            resources:
              requests:
                cpu: {{.RequestCpu}}m
                memory: {{.RequestMem}}Mi
              limits:
                cpu: {{.LimitCpu}}m
                memory: {{.LimitMem}}Mi
            command:
            - ./{{.ServiceName}}
            - -f
            - ./{{.Name}}.yaml
            volumeMounts:
            - name: timezone
              mountPath: /etc/localtime
          volumes:
          - name: timezone
            hostPath:
              path: /usr/share/zoneinfo/Asia/Shanghai
