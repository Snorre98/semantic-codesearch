// Shape interface for geometric objects.
export interface Shape {
  area(): number;
  perimeter(): number;
}

// A circle implementation of Shape.
export class Circle implements Shape {
  constructor(public radius: number) {}

  area(): number {
    return Math.PI * this.radius ** 2;
  }

  perimeter(): number {
    return 2 * Math.PI * this.radius;
  }
}

// Type alias for coordinate pairs.
export type Point = {
  x: number;
  y: number;
};

// Calculate the distance between two points.
export function distance(a: Point, b: Point): number {
  return Math.sqrt((b.x - a.x) ** 2 + (b.y - a.y) ** 2);
}

const DEFAULT_ORIGIN: Point = { x: 0, y: 0 };
