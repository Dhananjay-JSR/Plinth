terraform {
  required_providers {
    docker = { source = "kreuzwerker/docker" }
  }
}

resource "docker_image" "this" {
  name         = var.instance.spec.image
  keep_locally = true # never delete an image other containers might use
}

resource "docker_container" "this" {
  name  = "plinth-example-${var.environment.name}-${var.instance_name}"
  image = docker_image.this.image_id
  env   = [for k, v in try(var.instance.spec.env, {}) : "${k}=${v}"]

  ports {
    internal = var.instance.spec.container_port
    external = var.instance.spec.host_port
    ip       = "127.0.0.1" # reachable from this machine only
  }

  networks_advanced {
    name = var.inputs.network.attributes.name
  }
}

output "default" {
  value = {
    attributes = { container = docker_container.this.name }
    interfaces = {
      http = { url = "http://127.0.0.1:${var.instance.spec.host_port}" }
    }
  }
}
