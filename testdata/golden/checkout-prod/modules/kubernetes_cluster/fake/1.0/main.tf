# Stands in for a real cluster: stores its attributes in state, no cloud calls.
resource "terraform_data" "cluster" {
  input = {
    cluster_name = var.instance.spec.cluster_name
    endpoint     = "https://${var.instance.spec.cluster_name}.${var.environment.name}.fake"
  }
}

output "default" {
  value = {
    attributes = terraform_data.cluster.output
    interfaces = {}
  }
}
