# Stands in for a real deployment: records what it would deploy and where.
resource "terraform_data" "deployment" {
  input = {
    name     = var.instance_name
    image    = var.instance.spec.image
    replicas = try(var.instance.spec.replicas, 1)
    env      = try(var.instance.spec.env, {})
    cluster  = var.inputs.kubernetes_details.attributes.endpoint
    cache    = try(var.inputs.cache.attributes.host, "none")
  }
}

output "default" {
  value = {
    attributes = { name = var.instance_name }
    interfaces = { http = { host = "${var.instance_name}.svc", port = var.instance.spec.port } }
  }
}
