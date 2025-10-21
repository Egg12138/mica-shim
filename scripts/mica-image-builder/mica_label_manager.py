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
        self.micran_anno_prex = "org.openeuler.micran"
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
                rendered_value = self._render_template(value, kwargs)
                if rendered_value is not None:
                    labels[f"org.opencontainer.image.{key}"] = rendered_value

        # Add pedestal-specific labels (using PedPrefix = "org.openeuler.micran.ped.")
        if pedestal and 'pedestal' in self.labels_config and pedestal in self.labels_config['pedestal']:
            pedestal_config = self.labels_config['pedestal'][pedestal]
            for key, value in pedestal_config.items():
                rendered_value = self._render_template(value, kwargs)
                if rendered_value is not None:
                    labels[f"{self.micran_anno_prex}.ped.{key}"] = rendered_value

        # Add OS-specific labels (using ContainerPrefix = "org.openeuler.micran.container.")
        if os_type and 'os' in self.labels_config and os_type in self.labels_config['os']:
            os_config = self.labels_config['os'][os_type]
            for key, value in os_config.items():
                rendered_value = self._render_template(value, kwargs)
                if rendered_value is not None:
                    labels[f"{self.micran_anno_prex}.container.{key}"] = rendered_value

        # Add compatibility labels (using new prefix: "org.openeuler.micran.compatibility.*")
        # Only add compatibility for the current OS if it exists
        if os_type and 'compatibility' in self.labels_config and os_type in self.labels_config['compatibility']:
            for key, value in self.labels_config['compatibility'][os_type].items():
                labels[f"{self.micran_anno_prex}.compatibility.{key}"] = value
        elif 'default-compatibility' in self.labels_config:
            # Fallback to default compatibility if OS-specific doesn't exist
            for key, value in self.labels_config['default-compatibility'].items():
                labels[f"{self.micran_anno_prex}.compatibility.{key}"] = value

        # Add custom labels from non-default sections (excluding default- sections)
        for section_name, section_config in self.labels_config.items():
            if not section_name.startswith('default-') and section_name not in ['base', 'pedestal', 'os', 'compatibility']:
                # This is a custom extension section
                for key, value in section_config.items():
                    rendered_value = self._render_template(value, kwargs)
                    if rendered_value is not None:
                        labels[f"{self.micran_anno_prex}.{section_name}.{key}"] = rendered_value

        return labels

    def _render_template(self, template, context):
        if not isinstance(template, str):
            return template

        context['timestamp'] = datetime.now().isoformat()

        for key, value in context.items():
            placeholder = f"{{{{{key}}}}}"
            if placeholder in template:
                template = template.replace(placeholder, str(value))

        # Check if there are any unresolved placeholders remaining
        if '{{' in template and '}}' in template:
            return None  # Skip this label as it has unresolved placeholders

        return template

    def format_docker_labels(self, labels):
        dockerfile_lines = []
        for key, value in labels.items():
            if value is not None:
                dockerfile_lines.append(f'LABEL {key}="{value}"')
        return '\n'.join(dockerfile_lines)

    def get_scratch_labels(self, pedestal, os_type):
        return self.generate_labels(
            pedestal=pedestal,
            os_type=os_type,
            image_type="scratch"
        )

    def get_final_labels(self, pedestal, os_type, xen_image_path=None, firmware_path="/firmware.elf", custom_description=None, zephyr_version="3.7.1", uniproton_version="latest"):
        # The xen_image_path parameter is kept for API compatibility but not used
        # because the annotation should contain the container path (/image.bin),
        # not the source path from build context
        context = {
            "image_type": "application",
            "xen_image_path": "/image.bin",
            "firmware_path": firmware_path,
            "description": custom_description or f"Mica {os_type} Container Image",
            "zephyr_version": zephyr_version,
            "uniproton_version": uniproton_version
        }
        return self.generate_labels(
            pedestal=pedestal,
            os_type=os_type,
            **context
        )
