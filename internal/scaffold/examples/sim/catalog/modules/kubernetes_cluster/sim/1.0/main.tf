# Simulated: terraform_data stores values in state and calls no cloud.
resource "terraform_data" "cluster" {
  input = {
    cluster_name = var.instance.spec.cluster_name
    node_count   = try(var.instance.spec.node_count, 2)
    endpoint     = "https://${var.instance.spec.cluster_name}.${var.environment.name}.sim"
  }
}

output "default" {
  value = {
    attributes = terraform_data.cluster.output
    interfaces = {}
  }
}
