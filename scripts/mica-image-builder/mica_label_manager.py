#!/usr/bin/env python3

# Prefer stdlib tomllib (Python 3.11+), fall back to tomli for older Pythons.
try:
    import tomllib  # type: ignore
except Exception:  # pragma: no cover
    import tomli as tomllib  # type: ignore
from datetime import datetime
from pathlib import Path

class MicaLabelManager:
    def __init__(self, config_path="mica-labels.toml"):
        # Resolve default config relative to this file to be robust to CWD.
        default_path = Path(__file__).absolute().parent / "mica-labels.toml"
        self.config_path = Path(config_path)
        if config_path == "mica-labels.toml":
            self.config_path = default_path
        self.labels_config = self._load_config()

    def _load_config(self):
        if not self.config_path.exists():
            raise FileNotFoundError(f"Label config not found: {self.config_path}")

        with open(self.config_path, 'rb') as f:
            return tomllib.load(f)

    def generate_labels(self, pedestal=None, os_type=None, **kwargs):
        labels = {}

        # Add base labels
        if 'base' in self.labels_config:
            for key, value in self.labels_config['base'].items():
                labels[f"org.opencontainer.image.{key}"] = self._render_template(value, kwargs)

        # Add pedestal-specific labels
        if pedestal and 'pedestal' in self.labels_config and pedestal in self.labels_config['pedestal']:
            pedestal_config = self.labels_config['pedestal'][pedestal]
            for key, value in pedestal_config.items():
                labels[f"org.openeuler.mica.{key}"] = self._render_template(value, kwargs)

        # Add OS-specific labels
        if os_type and 'os' in self.labels_config and os_type in self.labels_config['os']:
            os_config = self.labels_config['os'][os_type]
            for key, value in os_config.items():
                labels[f"org.openeuler.mica.{key}"] = self._render_template(value, kwargs)

        # Add compatibility labels
        if 'compatibility' in self.labels_config:
            for key, value in self.labels_config['compatibility'].items():
                labels[f"org.openeuler.mica.compatibility.{key}"] = value

        return labels

    def _render_template(self, template, context):
        if not isinstance(template, str):
            return template

        context['timestamp'] = datetime.now().isoformat()

        for key, value in context.items():
            placeholder = f"{{{{{key}}}}}"
            if placeholder in template:
                template = template.replace(placeholder, str(value))

        return template

    def format_docker_labels(self, labels):
        dockerfile_lines = []
        for key, value in labels.items():
            dockerfile_lines.append(f'LABEL {key}="{value}"')
        return '\n'.join(dockerfile_lines)

    def get_scratch_labels(self, pedestal, os_type):
        return self.generate_labels(
            pedestal=pedestal,
            os_type=os_type,
            image_type="scratch"
        )

    def get_final_labels(self, pedestal, os_type, xen_image_path=None):
        context = {
            "image_type": "application",
            "xen_image_path": xen_image_path or "/image.bin"
        }
        return self.generate_labels(
            pedestal=pedestal,
            os_type=os_type,
            **context
        )
