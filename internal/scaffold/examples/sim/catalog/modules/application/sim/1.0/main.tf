# Simulated: records what would be deployed, and where.
resource "terraform_data" "deployment" {
  input = {
    image    = var.instance.spec.image
    replicas = try(var.instance.spec.replicas, 1)
    env      = try(var.instance.spec.env, {})
    cluster  = var.inputs.kubernetes_details.attributes.endpoint
  }
}

output "default" {
  value = {
    attributes = { name = var.instance_name }
    interfaces = { http = { host = "${var.instance_name}.svc", port = var.instance.spec.port } }
  }
}
