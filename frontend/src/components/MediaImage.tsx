import { useEffect, useState } from "react";
import { api } from "../api";
import type { Media } from "../types";

export default function MediaImage({
  id,
  alt,
  className,
}: {
  id: string;
  alt: string;
  className?: string;
}) {
  const [image, setImage] = useState<{ id: string; url: string } | null>(null);
  useEffect(() => {
    if (!id) return;
    let current = true;
    api<Media>(`/media/${id}`)
      .then((media) => {
        if (current && media?.url) setImage({ id, url: media.url });
      })
      .catch(() => {});
    return () => {
      current = false;
    };
  }, [id]);
  return image?.id === id ? (
    <img
      className={className}
      alt={alt}
      src={image.url}
      onError={() => setImage(null)}
    />
  ) : null;
}
