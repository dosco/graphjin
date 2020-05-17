import React from "react";
import { Redirect } from "@docusaurus/router";

// import classnames from "classnames";
// import Layout from "@theme/Layout";
// import Link from "@docusaurus/Link";
// import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
// import useBaseUrl from "@docusaurus/useBaseUrl";
// import styles from "./styles.module.css";
// import "../css/tailwind.css";

const Home = () => <Redirect to="/docs/home" />;

// function Feature({ imageUrl, title, description }) {
//   const imgUrl = useBaseUrl(imageUrl);
//   return (
//     <div className={classnames("col col--4", styles.feature)}>
//       {imgUrl && (
//         <div className="text--center">
//           <img className={styles.featureImage} src={imgUrl} alt={title} />
//         </div>
//       )}
//       <h3>{title}</h3>
//       <p>{description}</p>
//     </div>
//   );
// }

// function Home() {
//   const context = useDocusaurusContext();
//   const { siteConfig = {} } = context;
//   return (
//     <Layout
//       title={`Hello from ${siteConfig.title}`}
//       description="Description will go into a meta tag in <head />"
//       className="font-sans bg-red-500"
//     >
//       <div className="font-sans">
//         <header>
//           <div className="flex flex-col items-start p-6">
//             <h1 className="text-3xl pt-4">{siteConfig.title}</h1>
//             <p className="text-xl pt-4">{siteConfig.tagline}</p>
//             <div className="pt-4">
//               <Link
//                 className="border rounded hover:bg-gray-200 p-4 m-4"
//                 to={useBaseUrl("docs/doc  1")}
//               >
//                 Get Started
//               </Link>
//               <Link
//                 className="border rounded hover:bg-gray-200 p-4 m-4 ml-0"
//                 to="https://github.com/dosco/super-graph"
//               >
//                 Github
//               </Link>
//             </div>
//           </div>
//         </header>
//         <main></main>
//       </div>
//     </Layout>
//   );
// }

export default Home;
