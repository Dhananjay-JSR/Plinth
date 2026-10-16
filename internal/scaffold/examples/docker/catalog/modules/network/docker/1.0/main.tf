terraform {
  required_providers {
    docker = { source = "kreuzwerker/docker" } # reaches Docker via DOCKER_HOST or the local socket
  }
}

resource "docker_network" "this" {
  name = "plinth-example-${var.environment.name}-${var.instance_name}"
}

output "default" {
  value = {
    attributes = { name = docker_network.this.name, id = docker_network.this.id }
    interfaces = {}
  }
}
